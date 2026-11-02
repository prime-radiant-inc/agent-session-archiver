package archive

import (
	"encoding/json"
	"os"
)

func writeBenchmarkManifest(path string, m Manifest) error {
	b, e := json.Marshal(m)
	if e != nil {
		return e
	}
	return os.WriteFile(path, b, 0600)
}
