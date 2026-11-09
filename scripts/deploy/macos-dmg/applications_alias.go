package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	"github.com/leaanthony/dmg/hfsplus"
)

// The pinned HFS+ writer allocates only data forks, in contiguous 4096-byte
// blocks. Reserve both forks in one file, then split its catalog extent at the
// block boundary. No allocation map entries or other files need to move.
const hfsBlockSize = 4096

type applicationsAlias struct {
	entry      *hfsplus.Entry
	finderInfo []byte
	dataSize   int
	iconSize   int
}

func readApplicationsAlias(assets string) (*applicationsAlias, error) {
	data, err := os.ReadFile(filepath.Join(assets, "Applications"))
	if err != nil {
		return nil, err
	}
	icon, err := os.ReadFile(filepath.Join(assets, "Applications:com.apple.ResourceFork"))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(icon) == 0 {
		return nil, fmt.Errorf("incomplete Applications Finder alias or custom icon")
	}
	// Same folder alias type and creator as the macOS packager. The image
	// extractor returns the bookmark and resource fork without FinderInfo.
	info := make([]byte, 32)
	copy(info, "fdrpMACS")
	const aliasAndCustomIcon = 0x8400
	binary.BigEndian.PutUint16(info[8:10], aliasAndCustomIcon)
	combined := make([]byte, blocksFor(len(data))*hfsBlockSize+len(icon))
	copy(combined, data)
	copy(combined[blocksFor(len(data))*hfsBlockSize:], icon)
	return &applicationsAlias{
		entry:      &hfsplus.Entry{Name: "Applications", Kind: hfsplus.KindFile, Data: combined},
		finderInfo: info, dataSize: len(data), iconSize: len(icon),
	}, nil
}

func blocksFor(size int) int { return (size + hfsBlockSize - 1) / hfsBlockSize }

// Offsets follow the HFS+ Volume Header and Catalog File records in TN1150.
// Validate the writer's block/node sizes and the reserved extent before editing.
func (a *applicationsAlias) attachFinderMetadata(image []byte) error {
	be := binary.BigEndian
	if len(image) < 1536 {
		return fmt.Errorf("missing HFS+ volume header")
	}
	header := image[1024:1536]
	if be.Uint16(header[:2]) != 0x482b || be.Uint32(header[40:44]) != hfsBlockSize {
		return fmt.Errorf("unexpected HFS+ writer format")
	}
	start := int(be.Uint32(header[288:292])) * hfsBlockSize
	size := int(be.Uint32(header[292:296])) * hfsBlockSize
	if start < hfsBlockSize || size < hfsBlockSize || start+size > len(image) ||
		be.Uint16(image[start+32:start+34]) != hfsBlockSize {
		return fmt.Errorf("unexpected HFS+ catalog layout")
	}
	for off := start; off < start+size; off += hfsBlockSize {
		node := image[off : off+hfsBlockSize]
		if node[8] != 0xff { // Leaf node.
			continue
		}
		count := int(be.Uint16(node[10:12]))
		for i := 0; i < count; i++ {
			slot := hfsBlockSize - 2*(i+1)
			if slot < 14 {
				return fmt.Errorf("invalid HFS+ catalog offset table")
			}
			at := int(be.Uint16(node[slot : slot+2]))
			if at < 14 || at+2 > slot {
				return fmt.Errorf("invalid HFS+ catalog record")
			}
			body := at + 2 + int(be.Uint16(node[at:at+2]))
			if body+12 > slot || be.Uint16(node[body:body+2]) != 2 ||
				be.Uint32(node[body+8:body+12]) != a.entry.CNID {
				continue
			}
			if body+248 > slot {
				return fmt.Errorf("truncated Applications catalog record")
			}
			record := node[body : body+248]
			dataFork := record[88:168]
			startBlock := be.Uint32(dataFork[16:20])
			dataBlocks, iconBlocks := blocksFor(a.dataSize), blocksFor(a.iconSize)
			if startBlock == 0 || be.Uint64(dataFork[:8]) != uint64(len(a.entry.Data)) ||
				be.Uint32(dataFork[12:16]) != uint32(dataBlocks+iconBlocks) ||
				be.Uint32(dataFork[20:24]) != uint32(dataBlocks+iconBlocks) {
				return fmt.Errorf("Applications fork is not the reserved contiguous extent")
			}
			copy(record[48:80], a.finderInfo)
			writeFork(dataFork, a.dataSize, startBlock, uint32(dataBlocks))
			writeFork(record[168:248], a.iconSize, startBlock+uint32(dataBlocks), uint32(iconBlocks))
			return nil
		}
	}
	return fmt.Errorf("Applications catalog record not found")
}

func writeFork(fork []byte, size int, startBlock, blockCount uint32) {
	be := binary.BigEndian
	be.PutUint64(fork[:8], uint64(size))
	be.PutUint32(fork[8:12], uint32(size))
	be.PutUint32(fork[12:16], blockCount)
	be.PutUint32(fork[16:20], startBlock)
	be.PutUint32(fork[20:24], blockCount)
}
