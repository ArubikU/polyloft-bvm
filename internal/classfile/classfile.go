package classfile

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
)

const (
	Magic        uint32 = 0x50464243
	MajorVersion uint16 = 1
	MinorVersion uint16 = 0

	KindModule uint8 = 1

	BundleProjectConfigPath = "polyloft.toml"
	BundleManifestPath      = "META-INF/polyloft.json"
	BundleTypesPath         = "META-INF/polyloft.types.json"
	BundleMagic             = "POLYLOFT-PFX"
)

type Header struct {
	Magic        uint32
	MinorVersion uint16
	MajorVersion uint16
	Kind         uint8
	Reserved     uint8
	Flags        uint16
	PayloadSize  uint32
	MetadataSize uint32
}

type BundleManifest struct {
	Magic        string `json:"magic"`
	MinorVersion uint16 `json:"minor_version"`
	MajorVersion uint16 `json:"major_version"`
	EntryPoint   string `json:"entry_point"`
}

func DefaultBundleManifest(entryPoint string) BundleManifest {
	return BundleManifest{
		Magic:        BundleMagic,
		MinorVersion: MinorVersion,
		MajorVersion: MajorVersion,
		EntryPoint:   entryPoint,
	}
}

func (m BundleManifest) Validate() error {
	if m.Magic != BundleMagic {
		return fmt.Errorf("invalid bundle manifest magic %q", m.Magic)
	}
	if m.MajorVersion != MajorVersion {
		return fmt.Errorf("unsupported bundle major version %d", m.MajorVersion)
	}
	if m.EntryPoint == "" {
		return fmt.Errorf("bundle manifest is missing entry point")
	}
	return nil
}

func MarshalBundleManifest(manifest BundleManifest) ([]byte, error) {
	return json.MarshalIndent(manifest, "", "  ")
}

func UnmarshalBundleManifest(data []byte) (BundleManifest, error) {
	var manifest BundleManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return BundleManifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return BundleManifest{}, err
	}
	return manifest, nil
}

func WriteModule(w io.Writer, fn *bytecode.Function, metadata []byte) error {
	if fn == nil {
		return fmt.Errorf("classfile: nil function")
	}
	return writeModuleClassFile(w, fn, metadata)
}

func ReadModule(r io.Reader) (*bytecode.Function, []byte, error) {
	return readModuleClassFile(r)
}
