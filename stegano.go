package tunnel

import (
	"archive/zip"
	"bytes"
	"crypto/sha512"
	"encoding/binary"
	"encoding/json"
	"io"
	"io/fs"
	"os"
)

type ZFS interface {
	fs.FS
	io.Closer

	// Files 文件列表。
	Files() []*zip.File
}

func OpenZFS(f string) (ZFS, error) {
	zf, err := os.Open(f)
	if err != nil {
		return nil, err
	}
	stat, err1 := zf.Stat()
	if err1 != nil {
		_ = zf.Close()
		return nil, err1
	}
	defer func() {
		if err != nil {
			_ = zf.Close()
		}
	}()

	filesize := stat.Size()   // 文件总大小
	offset := 8 + sha512.Size // zip_size + hash_size
	payload := make([]byte, offset)
	_, err = zf.Seek(-int64(offset), io.SeekEnd)
	if _, err = io.ReadFull(zf, payload); err != nil {
		return nil, err
	}
	zipSize := int64(binary.LittleEndian.Uint64(payload[:8]))
	dataSize := zipSize + int64(offset)
	if dataSize < 0 || dataSize > filesize {
		return nil, zip.ErrFormat
	}

	section := io.NewSectionReader(zf, filesize-dataSize, zipSize)
	h := sha512.New()
	if _, err = io.Copy(h, section); err != nil {
		return nil, err
	}
	sum := h.Sum(nil)
	if !bytes.Equal(sum, payload[8:]) {
		return nil, zip.ErrChecksum
	}
	zr, err2 := zip.NewReader(section, zipSize)
	if err2 != nil {
		err = err2
		return nil, err2
	}
	zfs := &zipFS{
		zf: zf,
		zr: zr,
	}

	return zfs, nil
}

func ReadManifest(exe string, v any) error {
	zfs, err := OpenZFS(exe)
	if err != nil {
		return err
	}
	defer zfs.Close()

	// manifest.json 为约定的隐写配置文件名字，不要随意改变。
	mf, err := zfs.Open("manifest.json")
	if err != nil {
		return err
	}
	defer mf.Close()

	return json.NewDecoder(mf).Decode(v)
}

type zipFS struct {
	zf *os.File
	zr *zip.Reader
}

func (z *zipFS) Open(name string) (fs.File, error) {
	return z.zr.Open(name)
}

func (z *zipFS) Files() []*zip.File {
	return z.zr.File
}

func (z *zipFS) Close() error {
	return z.zf.Close()
}
