package diskcopy

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// StandaloneQCOW2 validates the qcow2 header before a hypervisor can open
// paths embedded in the image. It returns the guest-visible disk size.
func StandaloneQCOW2(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	size, err := qcow2Size(file)
	if err != nil {
		return 0, fmt.Errorf("invalid standalone qcow2 image: %w", err)
	}
	return size, nil
}

func qcow2Size(file io.Reader) (int64, error) {
	var header [104]byte
	if _, err := io.ReadFull(file, header[:72]); err != nil {
		return 0, err
	}
	if string(header[:4]) != "QFI\xfb" {
		return 0, fmt.Errorf("invalid magic")
	}
	be := binary.BigEndian
	if be.Uint64(header[8:16]) != 0 || be.Uint32(header[16:20]) != 0 {
		return 0, fmt.Errorf("backing files are not supported")
	}
	clusterBits := be.Uint32(header[20:24])
	if clusterBits < 9 || clusterBits > 21 {
		return 0, fmt.Errorf("invalid cluster size")
	}
	clusterSize := int64(1) << clusterBits
	size := int64(be.Uint64(header[24:32]))
	if size <= 0 {
		return 0, fmt.Errorf("invalid virtual size")
	}
	headerSize := int64(72)
	switch version := be.Uint32(header[4:8]); version {
	case 2:
	case 3:
		if _, err := io.ReadFull(file, header[72:]); err != nil {
			return 0, err
		}
		if be.Uint64(header[72:80])&(1<<2) != 0 {
			return 0, fmt.Errorf("external data files are not supported")
		}
		headerSize = int64(be.Uint32(header[100:104]))
		if headerSize < int64(len(header)) || headerSize > clusterSize || headerSize%8 != 0 {
			return 0, fmt.Errorf("invalid header size")
		}
		if _, err := io.CopyN(io.Discard, file, headerSize-int64(len(header))); err != nil {
			return 0, err
		}
	default:
		return 0, fmt.Errorf("unsupported qcow2 version %d", version)
	}
	// Header extensions reside in the rest of the first cluster. Reject an
	// external filename even when its corresponding feature bit is clear.
	var extension [8]byte
	for remaining := clusterSize - headerSize; remaining >= int64(len(extension)); {
		if _, err := io.ReadFull(file, extension[:]); err != nil {
			return 0, err
		}
		kind := be.Uint32(extension[:4])
		switch kind {
		case 0:
			return size, nil
		case 0x44415441:
			return 0, fmt.Errorf("external data files are not supported")
		}
		padded := (int64(be.Uint32(extension[4:])) + 7) &^ 7
		remaining -= int64(len(extension)) + padded
		if remaining < 0 {
			return 0, fmt.Errorf("invalid header extension size")
		}
		if _, err := io.CopyN(io.Discard, file, padded); err != nil {
			return 0, err
		}
	}
	return size, nil
}
