package rule

import (
	"container/list"
	"sync"
	"time"
)

const (
	// entityEvaluationFenceIdleCapacity bounds recent per-entity revision
	// watermarks. The fixed 65k ceiling covers reconnect/redelivery bursts
	// without allowing this safety cache to become an unbounded entity index.
	entityEvaluationFenceIdleCapacity = 65_536
	// entityEvaluationFenceIdleTTL keeps a recently idle watermark long enough
	// for normal watcher overlap and reconnect replay. It is a dedupe horizon,
	// not an operator retention policy or source-of-truth history window.
	entityEvaluationFenceIdleTTL = 15 * time.Minute
)

// entityEvaluationFence serializes state fetch, rule evaluation, and delete
// cleanup for one entity. Active entries are reference-counted and never
// evicted. When the last queued/in-flight reference leaves, the lock and its
// revision watermark move to a bounded TTL+LRU cache so sequential overlapping
// watcher deliveries remain ordered without creating an unbounded index.
type entityEvaluationFence struct {
	mu      sync.Mutex
	entries map[string]*entityEvaluationFenceEntry
	idleLRU *list.List

	// Test seams are zero in production, selecting the constants above.
	now          func() time.Time
	idleTTL      time.Duration
	idleCapacity int
}

type entityEvaluationFenceEntry struct {
	mu                  sync.Mutex
	refs                int
	latestRevision      uint64
	zeroDeleteProcessed bool
	idleSince           time.Time
	idleElement         *list.Element
}

func (f *entityEvaluationFence) retain(entityID string) *entityEvaluationFenceEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureLocked()
	now := f.clockLocked()()
	f.pruneExpiredLocked(now)
	entry := f.entries[entityID]
	if entry == nil {
		entry = &entityEvaluationFenceEntry{}
		f.entries[entityID] = entry
	} else if entry.refs == 0 {
		f.removeIdleLocked(entry)
	}
	entry.refs++
	return entry
}

func (f *entityEvaluationFence) release(entityID string, entry *entityEvaluationFenceEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureLocked()
	if entry == nil || f.entries[entityID] != entry || entry.refs == 0 {
		return
	}
	entry.refs--
	if entry.refs == 0 {
		f.makeIdleLocked(entityID, entry, f.clockLocked()())
	}
}

func (f *entityEvaluationFence) releaseRetained(entityID string, count int) {
	if count <= 0 {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureLocked()
	entry := f.entries[entityID]
	if entry == nil || count > entry.refs {
		return
	}
	entry.refs -= count
	if entry.refs == 0 {
		f.makeIdleLocked(entityID, entry, f.clockLocked()())
	}
}

func (f *entityEvaluationFence) makeIdleLocked(
	entityID string,
	entry *entityEvaluationFenceEntry,
	now time.Time,
) {
	entry.idleSince = now
	entry.idleElement = f.idleLRU.PushBack(entityID)
	f.pruneExpiredLocked(now)
	for f.idleLRU.Len() > f.capacityLocked() {
		f.evictOldestIdleLocked()
	}
}

func (f *entityEvaluationFence) pruneExpiredLocked(now time.Time) {
	ttl := f.ttlLocked()
	for element := f.idleLRU.Front(); element != nil; element = f.idleLRU.Front() {
		entityID, ok := element.Value.(string)
		if !ok {
			f.idleLRU.Remove(element)
			continue
		}
		entry := f.entries[entityID]
		if entry == nil || entry.refs != 0 || entry.idleElement != element {
			f.idleLRU.Remove(element)
			continue
		}
		if now.Before(entry.idleSince) || now.Sub(entry.idleSince) < ttl {
			return
		}
		f.idleLRU.Remove(element)
		delete(f.entries, entityID)
	}
}

func (f *entityEvaluationFence) evictOldestIdleLocked() {
	element := f.idleLRU.Front()
	if element == nil {
		return
	}
	entityID, _ := element.Value.(string)
	f.idleLRU.Remove(element)
	entry := f.entries[entityID]
	if entry != nil && entry.refs == 0 && entry.idleElement == element {
		delete(f.entries, entityID)
	}
}

func (f *entityEvaluationFence) removeIdleLocked(entry *entityEvaluationFenceEntry) {
	if entry.idleElement != nil {
		f.idleLRU.Remove(entry.idleElement)
		entry.idleElement = nil
	}
	entry.idleSince = time.Time{}
}

// clearIdle drops retained watermarks at processor shutdown. Active entries
// remain visible rather than being silently erased; after watcher retirement
// and coalescer drain the active count must be zero.
func (f *entityEvaluationFence) clearIdle() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureLocked()
	for element := f.idleLRU.Front(); element != nil; {
		next := element.Next()
		entityID, _ := element.Value.(string)
		entry := f.entries[entityID]
		if entry != nil && entry.refs == 0 {
			delete(f.entries, entityID)
		}
		element = next
	}
	f.idleLRU.Init()
}

func (f *entityEvaluationFence) counts() (active, idle int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureLocked()
	for _, entry := range f.entries {
		if entry.refs > 0 {
			active++
		} else {
			idle++
		}
	}
	return active, idle
}

func (f *entityEvaluationFence) ensureLocked() {
	if f.entries == nil {
		f.entries = make(map[string]*entityEvaluationFenceEntry)
	}
	if f.idleLRU == nil {
		f.idleLRU = list.New()
	}
}

func (f *entityEvaluationFence) clockLocked() func() time.Time {
	if f.now != nil {
		return f.now
	}
	return time.Now
}

func (f *entityEvaluationFence) ttlLocked() time.Duration {
	if f.idleTTL > 0 {
		return f.idleTTL
	}
	return entityEvaluationFenceIdleTTL
}

func (f *entityEvaluationFence) capacityLocked() int {
	if f.idleCapacity > 0 {
		return f.idleCapacity
	}
	return entityEvaluationFenceIdleCapacity
}

func (e *entityEvaluationFenceEntry) admits(snapshot entitySnapshot) bool {
	if snapshot.Revision > 0 {
		return snapshot.Revision > e.latestRevision
	}
	return snapshot.Action != "DELETED" || !e.zeroDeleteProcessed
}

func (e *entityEvaluationFenceEntry) record(snapshot entitySnapshot) {
	if snapshot.Revision > e.latestRevision {
		e.latestRevision = snapshot.Revision
	}
	if snapshot.Action == "DELETED" {
		e.zeroDeleteProcessed = true
	} else if snapshot.Revision > 0 {
		e.zeroDeleteProcessed = false
	}
}
