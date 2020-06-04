package zsync

import (
	"appimage-update/src/zsync/chunks"
	"appimage-update/src/zsync/circularbuffer"
	"appimage-update/src/zsync/sources"
	"fmt"
	"github.com/schollz/progressbar/v3"
	"io"
	"math"
)

type ChunkLookupSlice struct {
	offset              int64
	chunkSize           int64
	chunkCount          int64
	lastFullChunkOffset int64
	fileSize            int64
	file                io.ReadSeeker
	buffer              *circularbuffer.C2
}

func NewChunkLookupSlice(file io.ReadSeeker, chunkSize int64) (*ChunkLookupSlice, error) {
	fileSize, err := file.Seek(0, 2)
	if err != nil {
		return nil, err
	}
	_, err = file.Seek(0, 0)

	chunkCount := int64(math.Ceil(float64(fileSize) / float64(chunkSize)))

	lookupSlice := &ChunkLookupSlice{
		offset:              0,
		chunkSize:           chunkSize,
		chunkCount:          chunkCount,
		lastFullChunkOffset: (fileSize / chunkSize) * chunkSize,
		fileSize:            fileSize,
		file:                file,
		buffer:              circularbuffer.MakeC2Buffer(int(chunkSize)),
	}
	return lookupSlice, nil
}

func (s *ChunkLookupSlice) isEOF() bool {
	return s.offset <= s.lastFullChunkOffset
}

func (s ChunkLookupSlice) getNextChunkSize() int64 {
	if s.offset+s.chunkSize > s.fileSize {
		return s.fileSize - s.offset
	} else {
		return s.chunkSize
	}
}

func (syncData *SyncData) identifyAllLocalMatchingChunks(matchingChunks []chunks.ChunkInfo) ([]chunks.ChunkInfo, error) {
	lookupSlice, err := NewChunkLookupSlice(syncData.Local, int64(syncData.BlockSize))
	if err != nil {
		return nil, err
	}

	progress := progressbar.DefaultBytes(
		lookupSlice.fileSize,
		"Searching reusable chunks: ",
	)

	for lookupSlice.isEOF() {
		_ = progress.Set(int(lookupSlice.offset))

		chunkSize := lookupSlice.getNextChunkSize()
		data, err := sources.ReadChunk(syncData.Local, lookupSlice.offset, chunkSize)
		if err != nil {
			return nil, err
		}

		if chunkSize < int64(syncData.BlockSize) {
			zeroChunk := make([]byte, int64(syncData.BlockSize)-chunkSize)
			data = append(data, zeroChunk...)
		}

		matches := syncData.searchMatchingChunks(data)
		if matches != nil {
			matchingChunks = syncData.appendMatchingChunks(matchingChunks, matches, chunkSize, lookupSlice.offset)
			lookupSlice.offset += int64(syncData.BlockSize)
		} else {
			lookupSlice.offset += 1
		}
	}
	_ = progress.Set(int(lookupSlice.fileSize))
	return matchingChunks, nil
}

func (syncData *SyncData) appendMatchingChunks(matchingChunks []chunks.ChunkInfo, matches []chunks.ChunkChecksum, chunkSize int64, offset int64) []chunks.ChunkInfo {
	for _, match := range matches {
		newChunk := chunks.ChunkInfo{
			Size:         chunkSize,
			Source:       syncData.Local,
			SourceOffset: offset,
			TargetOffset: int64(match.ChunkOffset * syncData.BlockSize),
		}

		// chop zero filled chunks at the end
		if newChunk.TargetOffset+newChunk.Size > syncData.FileLength {
			newChunk.Size = syncData.FileLength - newChunk.TargetOffset
		}
		matchingChunks = append(matchingChunks, newChunk)
	}
	return matchingChunks
}
