package rag

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestBuildMaterialChunksFromPDF_InvalidPathReturnsNil(t *testing.T) {
	sessionID := uuid.New()
	materialID := uuid.New()
	out := BuildMaterialChunksFromPDF(sessionID, materialID, "/nonexistent/path/to/file.pdf")
	assert.Nil(t, out)
}

func TestBuildMaterialChunksFromPDF_EmptyPathReturnsNil(t *testing.T) {
	sessionID := uuid.New()
	materialID := uuid.New()
	out := BuildMaterialChunksFromPDF(sessionID, materialID, "")
	assert.Nil(t, out)
}
