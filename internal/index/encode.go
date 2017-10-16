package index

import (
	"encoding/binary"
	"fmt"
	"io"
	"sort"
)

type countingWriter struct {
	offset uint32
	w      io.Writer
}

func (cw *countingWriter) Write(p []byte) (n int, err error) {
	n, err = cw.w.Write(p)
	cw.offset += uint32(n)
	return n, err
}

func encode(w io.Writer, idx map[string]string) error {
	cw := countingWriter{w: w}
	w = io.Writer(&cw)

	vals := make([]string, 0, len(idx))
	for _, val := range idx {
		vals = append(vals, val)
	}
	sort.Strings(vals) // for a deterministic index file
	valOffsets := make(map[string]uint32, len(vals))
	for _, val := range vals {
		if _, written := valOffsets[val]; written {
			continue
		}
		valOffsets[val] = cw.offset
		if _, err := fmt.Fprintln(w, val); err != nil {
			return err
		}
	}

	byLength := make(map[int][]string)
	var highest int
	for key := range idx {
		l := len(key)
		byLength[l] = append(byLength[l], key)
		if l > highest {
			highest = l
		}
	}
	// Fill in the gaps so that lookups can seek+read instead of having to
	// binary search through same-length-blocks.
	for i := 1; i <= highest; i++ {
		if _, ok := byLength[i]; ok {
			continue
		}
		byLength[i] = []string(nil)
	}
	var lengths []int
	for l := range byLength {
		lengths = append(lengths, l)
	}
	sort.Ints(lengths) // for a deterministic index file
	sameLenOffsets := make(map[int]uint32)
	// Write same-length-block:
	// <key><offset>
	// <key><offset>
	// …
	// Where each <key> has the same length.
	for _, l := range lengths {
		sameLenOffsets[l] = cw.offset
		keys := byLength[l]
		sort.Strings(keys)
		for _, k := range keys {
			if _, err := w.Write([]byte(k)); err != nil {
				return err
			}
			if err := binary.Write(w, binary.LittleEndian, valOffsets[idx[k]]); err != nil {
				return err
			}
		}
	}

	blockIndexOffset := cw.offset
	// Write block index (position == key length):
	// uint32(<same-len-block-offset>), uint32(<same-len-block-len>)

	// So that the length of the current block can be computed by looking at the
	// offset of the next block:
	sameLenOffsets[lengths[len(lengths)-1]+1] = cw.offset
	for _, l := range lengths {
		blockLen := sameLenOffsets[l+1] - sameLenOffsets[l]
		blockOffset := sameLenOffsets[l]
		if err := binary.Write(w, binary.LittleEndian, BlockLocation{blockOffset, blockLen}); err != nil {
			return err
		}
	}

	return binary.Write(w, binary.LittleEndian, blockIndexOffset)
}

func (index URIs) Encode(w io.Writer) error {
	idx := make(map[string]string, len(index))
	for src, dsc := range index {
		idx[fmt.Sprintf("%s\t%s", src.Package, src.Version)] = fmt.Sprintf("%s\t%d", dsc.URL, dsc.Size)
	}
	return encode(w, idx)
}

func (index Index) Encode(w io.Writer) error {
	idx := make(map[string]string, len(index))
	for key, src := range index {
		idx[key] = fmt.Sprintf("%s\t%s", src.Package, src.Version.String())
	}
	return encode(w, idx)
}
