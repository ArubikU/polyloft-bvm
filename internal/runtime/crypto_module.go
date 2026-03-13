package runtime

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"

	"github.com/ArubikU/polyloft-bvm/internal/value"
)

func BuildCryptoModule() *RuntimeModule {
	builder := NewModuleBuilder("Crypto")

	builder.AddTypedFunction("md5", []string{TypeString}, TypeString, false, func(args []value.Value) (value.Value, error) {
		sum := md5.Sum([]byte(args[0].Str))
		return value.StringValue(hex.EncodeToString(sum[:])), nil
	})

	builder.AddTypedFunction("sha1", []string{TypeString}, TypeString, false, func(args []value.Value) (value.Value, error) {
		sum := sha1.Sum([]byte(args[0].Str))
		return value.StringValue(hex.EncodeToString(sum[:])), nil
	})

	builder.AddTypedFunction("sha256", []string{TypeString}, TypeString, false, func(args []value.Value) (value.Value, error) {
		sum := sha256.Sum256([]byte(args[0].Str))
		return value.StringValue(hex.EncodeToString(sum[:])), nil
	})

	builder.AddTypedFunction("sha512", []string{TypeString}, TypeString, false, func(args []value.Value) (value.Value, error) {
		sum := sha512.Sum512([]byte(args[0].Str))
		return value.StringValue(hex.EncodeToString(sum[:])), nil
	})

	builder.AddTypedFunction("base64_encode", []string{TypeString}, TypeString, false, func(args []value.Value) (value.Value, error) {
		return value.StringValue(base64.StdEncoding.EncodeToString([]byte(args[0].Str))), nil
	})

	builder.AddTypedFunction("base64_decode", []string{TypeString}, TypeString, false, func(args []value.Value) (value.Value, error) {
		decoded, err := base64.StdEncoding.DecodeString(args[0].Str)
		if err != nil {
			return value.NilValue(), err
		}
		return value.StringValue(string(decoded)), nil
	})

	builder.AddTypedFunction("hex_encode", []string{TypeString}, TypeString, false, func(args []value.Value) (value.Value, error) {
		return value.StringValue(hex.EncodeToString([]byte(args[0].Str))), nil
	})

	builder.AddTypedFunction("hex_decode", []string{TypeString}, TypeString, false, func(args []value.Value) (value.Value, error) {
		decoded, err := hex.DecodeString(args[0].Str)
		if err != nil {
			return value.NilValue(), err
		}
		return value.StringValue(string(decoded)), nil
	})

	return builder.Build()
}
