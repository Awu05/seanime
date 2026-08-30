package mkvparser

import (
	"bytes"
	"context"
	"seanime/internal/util"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// vintEncodeForTest encodes a uint64 as an EBML variable-length integer, for building
// synthetic Matroska byte streams in tests.
func vintEncodeForTest(value uint64) []byte {
	var length int
	switch {
	case value < 0x80:
		length = 1
	case value < 0x4000:
		length = 2
	case value < 0x200000:
		length = 3
	default:
		length = 4
	}
	buf := make([]byte, length)
	switch length {
	case 1:
		buf[0] = byte(value) | 0x80
	case 2:
		buf[0] = byte(value>>8) | 0x40
		buf[1] = byte(value)
	case 3:
		buf[0] = byte(value>>16) | 0x20
		buf[1] = byte(value >> 8)
		buf[2] = byte(value)
	case 4:
		buf[0] = byte(value>>24) | 0x10
		buf[1] = byte(value >> 16)
		buf[2] = byte(value >> 8)
		buf[3] = byte(value)
	}
	return buf
}

// buildTestMatroskaWithAttachment builds a minimal but valid Matroska byte stream containing a
// SegmentInfo, a single video TrackEntry, and an Attachments block with one embedded file -
// enough to exercise GetMetadata's track extraction and the separate attachment lookup, without
// needing a real .mkv fixture or network/torrent access.
func buildTestMatroskaWithAttachment(attachmentName string, attachmentData []byte) []byte {
	buf := new(bytes.Buffer)

	// EBML Header
	ebmlHeader := new(bytes.Buffer)
	ebmlHeader.Write([]byte{0x42, 0x82, 0x88, 'm', 'a', 't', 'r', 'o', 's', 'k', 'a'}) // DocType
	buf.Write([]byte{0x1A, 0x45, 0xDF, 0xA3})
	buf.Write(vintEncodeForTest(uint64(ebmlHeader.Len())))
	buf.Write(ebmlHeader.Bytes())

	segment := new(bytes.Buffer)

	// SegmentInfo: Title + TimestampScale (1ms)
	segInfo := new(bytes.Buffer)
	segInfo.Write([]byte{0x7B, 0xA9, 0x8A, 'T', 'e', 's', 't', ' ', 'T', 'i', 't', 'l', 'e'})
	segInfo.Write([]byte{0x2A, 0xD7, 0xB1, 0x83, 0x0F, 0x42, 0x40}) // TimestampScale = 1,000,000
	segment.Write([]byte{0x15, 0x49, 0xA9, 0x66})
	segment.Write(vintEncodeForTest(uint64(segInfo.Len())))
	segment.Write(segInfo.Bytes())

	// Tracks: one minimal video TrackEntry
	trackEntry := new(bytes.Buffer)
	trackEntry.Write([]byte{0xD7, 0x81, 0x01})                         // TrackNumber = 1
	trackEntry.Write([]byte{0x73, 0xC5, 0x88, 0, 0, 0, 0, 0, 0, 0, 1}) // TrackUID = 1
	trackEntry.Write([]byte{0x83, 0x81, 0x01})                         // TrackType = video
	trackEntry.WriteByte(0x86)                                         // CodecID
	trackEntry.Write(vintEncodeForTest(uint64(len("V_TEST"))))
	trackEntry.WriteString("V_TEST")
	videoBuf := new(bytes.Buffer)
	videoBuf.Write([]byte{0xB0, 0x82, 0x07, 0x80}) // PixelWidth = 1920
	videoBuf.Write([]byte{0xBA, 0x82, 0x04, 0x38}) // PixelHeight = 1080
	trackEntry.WriteByte(0xE0)                     // Video element
	trackEntry.Write(vintEncodeForTest(uint64(videoBuf.Len())))
	trackEntry.Write(videoBuf.Bytes())

	tracks := new(bytes.Buffer)
	tracks.Write([]byte{0xAE}) // TrackEntry ID
	tracks.Write(vintEncodeForTest(uint64(trackEntry.Len())))
	tracks.Write(trackEntry.Bytes())
	segment.Write([]byte{0x16, 0x54, 0xAE, 0x6B})
	segment.Write(vintEncodeForTest(uint64(tracks.Len())))
	segment.Write(tracks.Bytes())

	// Attachments: one embedded file
	attachedFile := new(bytes.Buffer)
	attachedFile.Write([]byte{0x46, 0x6E}) // FileName
	attachedFile.Write(vintEncodeForTest(uint64(len(attachmentName))))
	attachedFile.WriteString(attachmentName)
	mime := "font/ttf"
	attachedFile.Write([]byte{0x46, 0x60}) // FileMimeType
	attachedFile.Write(vintEncodeForTest(uint64(len(mime))))
	attachedFile.WriteString(mime)
	attachedFile.Write([]byte{0x46, 0x5C}) // FileData
	attachedFile.Write(vintEncodeForTest(uint64(len(attachmentData))))
	attachedFile.Write(attachmentData)
	attachedFile.Write([]byte{0x46, 0xAE, 0x81, 0x01}) // FileUID = 1

	attachments := new(bytes.Buffer)
	attachments.Write([]byte{0x61, 0xA7}) // AttachedFile ID
	attachments.Write(vintEncodeForTest(uint64(attachedFile.Len())))
	attachments.Write(attachedFile.Bytes())

	segment.Write([]byte{0x19, 0x41, 0xA4, 0x69})
	segment.Write(vintEncodeForTest(uint64(attachments.Len())))
	segment.Write(attachments.Bytes())

	buf.Write([]byte{0x18, 0x53, 0x80, 0x67})
	buf.Write(vintEncodeForTest(uint64(segment.Len())))
	buf.Write(segment.Bytes())

	return buf.Bytes()
}

func TestMetadataParser_GetMetadata_DoesNotEagerlyParseAttachments(t *testing.T) {
	data := buildTestMatroskaWithAttachment("font.ttf", []byte("fake-font-bytes"))
	parser := NewMetadataParser(bytes.NewReader(data), util.NewLogger())

	metadata := parser.GetMetadata(context.Background())
	require.NoError(t, metadata.Error)

	assert.Empty(t, metadata.Attachments, "GetMetadata should not eagerly read the Attachments block")
}

func TestMetadataParser_GetAttachmentByName_LazilyFetchesAttachment(t *testing.T) {
	data := buildTestMatroskaWithAttachment("font.ttf", []byte("fake-font-bytes"))
	parser := NewMetadataParser(bytes.NewReader(data), util.NewLogger())

	metadata := parser.GetMetadata(context.Background())
	require.NoError(t, metadata.Error)
	require.Empty(t, metadata.Attachments)

	attachment, ok := parser.GetAttachmentByName(context.Background(), "font.ttf")
	require.True(t, ok, "expected to lazily find the attachment by name")
	assert.Equal(t, "font.ttf", attachment.Filename)
	assert.Equal(t, "fake-font-bytes", string(attachment.Data))

	_, ok = parser.GetAttachmentByName(context.Background(), "does-not-exist.ttf")
	assert.False(t, ok)
}
