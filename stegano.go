package tunnel

import (
	"archive/zip"
	"encoding/json"
)

func OpenFS(f string) (*zip.ReadCloser, error) {
	return zip.OpenReader(f)
}

func ReadManifest(f string, v any) error {
	zrc, err := OpenFS(f)
	if err != nil {
		return err
	}
	defer zrc.Close()

	// manifest.json 为约定的隐写配置文件名字，不要随意改变。
	mf, err := zrc.Open("manifest.json")
	if err != nil {
		return err
	}
	defer mf.Close()

	return json.NewDecoder(mf).Decode(v)
}
