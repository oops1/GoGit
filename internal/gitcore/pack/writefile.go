package pack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

var fileClose = func(file *os.File) error { return file.Close() }

func WritePackFile(ctx context.Context, dir string, src ObjectSource, ids []hash.ObjectID, opts WriteOptions) (IndexResult, error) {
	temp, tempPath, err := createTempFile(dir, tempPackPattern)
	if err != nil {
		return IndexResult{}, err
	}
	renamed := false
	defer cleanupTempFile(temp, tempPath, &renamed)

	written, err := WritePack(ctx, temp, src, ids, opts)
	if err != nil {
		return IndexResult{}, err
	}
	if err := fileClose(temp); err != nil {
		return IndexResult{}, fmt.Errorf("pack: close %s: %w", tempPath, err)
	}
	base := filepath.Join(dir, "pack-"+written.Checksum.String())
	reused, err := placeFile(tempPath, base+packSuffix)
	if err != nil {
		return IndexResult{}, err
	}
	renamed = !reused
	if err := writeIndexFile(dir, base+indexSuffix, written.Entries, written.Checksum); err != nil {
		return IndexResult{}, err
	}
	return IndexResult{
		Checksum:  written.Checksum,
		PackPath:  base + packSuffix,
		IndexPath: base + indexSuffix,
		Objects:   written.Objects,
		Bytes:     written.Bytes,
	}, nil
}
