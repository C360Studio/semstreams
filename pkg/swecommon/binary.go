package swecommon

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// BinaryByteOrder is the wire byte order for the binary encoding.
// Default and recommendation: big-endian (network byte order). The
// producer and consumer must agree; the schema's BinaryEncoding
// metadata block carries the choice on the wire.
type BinaryByteOrder uint8

const (
	// BigEndian — default. Matches network byte order.
	BigEndian BinaryByteOrder = iota
	// LittleEndian — opt-in for producers that want to skip the
	// host-to-network byte swap on x86/ARM.
	LittleEndian
)

func (b BinaryByteOrder) order() binary.ByteOrder {
	if b == LittleEndian {
		return binary.LittleEndian
	}
	return binary.BigEndian
}

// BinaryEncoding configures the SWE BinaryEncoding (SWE Common
// §7.4.4) options. The framework Phase 1 supports the
// "packed primitives" variant: each field is encoded back-to-back
// with no padding, types per the wire format below.
//
// Per-field wire format:
//
//   - Quantity → 8 bytes IEEE 754 float64
//   - Count    → 8 bytes int64
//   - Time     → uint32 length prefix + UTF-8 RFC 3339 bytes
//   - Boolean  → 1 byte (0 = false, 1 = true)
//   - Text     → uint32 length prefix + UTF-8 bytes
//   - Category → uint32 length prefix + UTF-8 bytes
//
// Nil values: a separate nil bitmap precedes each record. The
// bitmap is ceil(nfields/8) bytes, big-endian bit order (high bit
// = field 0). A 1 bit means the field is nil; the value bytes are
// skipped for nil fields.
type BinaryEncoding struct {
	ByteOrder BinaryByteOrder
}

// DefaultBinaryEncoding returns the network-byte-order packed
// primitives encoding.
func DefaultBinaryEncoding() BinaryEncoding {
	return BinaryEncoding{ByteOrder: BigEndian}
}

// EncodeBinary writes the rows as SWE BinaryEncoding bytes per the
// schema and encoding configuration. Field order follows the
// schema's field declaration order. Each record is preceded by a
// nil bitmap (see [BinaryEncoding] for the bitmap layout).
//
// No record-count prefix is emitted — consumers read until EOF.
// This matches the streaming-friendly shape CS API uses for
// chunked observation responses.
func EncodeBinary(w io.Writer, schema *DataRecord, rows []Values, enc BinaryEncoding) error {
	if err := schema.Validate(); err != nil {
		return fmt.Errorf("swecommon: encode binary: %w", err)
	}
	order := enc.ByteOrder.order()
	bw := &binaryWriter{w: w, order: order}
	for ri, row := range rows {
		if err := binaryWriteRow(bw, schema, row); err != nil {
			return fmt.Errorf("swecommon: encode binary row %d: %w", ri, err)
		}
	}
	return bw.err
}

// MarshalBinaryRows is the bytes-returning convenience wrapper around
// [EncodeBinary].
func MarshalBinaryRows(schema *DataRecord, rows []Values, enc BinaryEncoding) ([]byte, error) {
	var buf bytes.Buffer
	if err := EncodeBinary(&buf, schema, rows, enc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func binaryWriteRow(bw *binaryWriter, schema *DataRecord, row Values) error {
	nilBitmap := make([]byte, (len(schema.Fields)+7)/8)
	canonValues := make([]any, len(schema.Fields))
	for i, f := range schema.Fields {
		v := fieldValue(row, f.Name)
		if matchesNilToken(f.Component, v) {
			v = nil
		}
		canon, isNil, err := normalizeValue(f.Component, v)
		if err != nil {
			return fmt.Errorf("field %q: %w", f.Name, err)
		}
		if isNil {
			nilBitmap[i/8] |= 1 << uint(7-(i%8))
			continue
		}
		canonValues[i] = canon
	}
	bw.writeBytes(nilBitmap)
	if bw.err != nil {
		return bw.err
	}
	for i, f := range schema.Fields {
		if nilBitmap[i/8]&(1<<uint(7-(i%8))) != 0 {
			continue
		}
		if err := binaryWriteField(bw, f.Component, canonValues[i]); err != nil {
			return fmt.Errorf("field %q: %w", f.Name, err)
		}
	}
	return bw.err
}

func binaryWriteField(bw *binaryWriter, c DataComponent, v any) error {
	switch c.Kind() {
	case KindQuantity:
		bw.writeUint64(math.Float64bits(v.(float64)))
	case KindCount:
		bw.writeUint64(uint64(v.(int64)))
	case KindBoolean:
		if v.(bool) {
			bw.writeBytes([]byte{1})
		} else {
			bw.writeBytes([]byte{0})
		}
	case KindTime, KindText, KindCategory:
		s := v.(string)
		if uint64(len(s)) > math.MaxUint32 {
			return fmt.Errorf("%s length %d exceeds uint32 prefix max (4 GiB)", c.Kind(), len(s))
		}
		bw.writeUint32(uint32(len(s)))
		bw.writeBytes([]byte(s))
	default:
		return fmt.Errorf("unsupported kind %q", c.Kind())
	}
	return bw.err
}

// DecodeBinary reads SWE BinaryEncoding bytes into typed rows
// against the schema. EOF after a complete record terminates the
// stream cleanly; EOF mid-record surfaces as
// [io.ErrUnexpectedEOF].
func DecodeBinary(r io.Reader, schema *DataRecord, enc BinaryEncoding) ([]Values, error) {
	if err := schema.Validate(); err != nil {
		return nil, fmt.Errorf("swecommon: decode binary: %w", err)
	}
	order := enc.ByteOrder.order()
	br := &binaryReader{r: r, order: order}
	bitmapLen := (len(schema.Fields) + 7) / 8
	out := make([]Values, 0)
	for {
		bitmap := make([]byte, bitmapLen)
		n, err := io.ReadFull(br.r, bitmap)
		if errors.Is(err, io.EOF) && n == 0 {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("swecommon: decode binary nil-bitmap: %w", err)
		}
		row := make(Values, len(schema.Fields))
		for i, f := range schema.Fields {
			if bitmap[i/8]&(1<<uint(7-(i%8))) != 0 {
				row[f.Name] = nil
				continue
			}
			v, err := binaryReadField(br, f.Component)
			if err != nil {
				return nil, fmt.Errorf("swecommon: decode binary row %d field %q: %w", len(out), f.Name, err)
			}
			row[f.Name] = v
		}
		out = append(out, row)
	}
}

// UnmarshalBinaryRows is the bytes-taking convenience wrapper around
// [DecodeBinary].
func UnmarshalBinaryRows(data []byte, schema *DataRecord, enc BinaryEncoding) ([]Values, error) {
	return DecodeBinary(bytes.NewReader(data), schema, enc)
}

func binaryReadField(br *binaryReader, c DataComponent) (any, error) {
	switch c.Kind() {
	case KindQuantity:
		bits, err := br.readUint64()
		if err != nil {
			return nil, fmt.Errorf("Quantity: %w", err)
		}
		return math.Float64frombits(bits), nil
	case KindCount:
		u, err := br.readUint64()
		if err != nil {
			return nil, fmt.Errorf("Count: %w", err)
		}
		return int64(u), nil
	case KindBoolean:
		b, err := br.readByte()
		if err != nil {
			return nil, fmt.Errorf("Boolean: %w", err)
		}
		return b != 0, nil
	case KindTime, KindText, KindCategory:
		n, err := br.readUint32()
		if err != nil {
			return nil, fmt.Errorf("%s length: %w", c.Kind(), err)
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(br.r, buf); err != nil {
			return nil, fmt.Errorf("%s bytes: %w", c.Kind(), err)
		}
		return string(buf), nil
	default:
		return nil, fmt.Errorf("unsupported kind %q", c.Kind())
	}
}

// binaryWriter is a sticky-error wrapper around io.Writer; once an
// error is set, further writes are no-ops so the caller can check
// the err once at the end.
type binaryWriter struct {
	w     io.Writer
	order binary.ByteOrder
	err   error
}

func (b *binaryWriter) writeBytes(p []byte) {
	if b.err != nil {
		return
	}
	_, b.err = b.w.Write(p)
}

func (b *binaryWriter) writeUint32(v uint32) {
	if b.err != nil {
		return
	}
	var buf [4]byte
	b.order.PutUint32(buf[:], v)
	_, b.err = b.w.Write(buf[:])
}

func (b *binaryWriter) writeUint64(v uint64) {
	if b.err != nil {
		return
	}
	var buf [8]byte
	b.order.PutUint64(buf[:], v)
	_, b.err = b.w.Write(buf[:])
}

// binaryReader mirrors binaryWriter on the read side.
type binaryReader struct {
	r     io.Reader
	order binary.ByteOrder
}

func (b *binaryReader) readByte() (byte, error) {
	var buf [1]byte
	if _, err := io.ReadFull(b.r, buf[:]); err != nil {
		return 0, err
	}
	return buf[0], nil
}

func (b *binaryReader) readUint32() (uint32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(b.r, buf[:]); err != nil {
		return 0, err
	}
	return b.order.Uint32(buf[:]), nil
}

func (b *binaryReader) readUint64() (uint64, error) {
	var buf [8]byte
	if _, err := io.ReadFull(b.r, buf[:]); err != nil {
		return 0, err
	}
	return b.order.Uint64(buf[:]), nil
}
