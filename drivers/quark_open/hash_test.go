package quark_open

import (
	"testing"

	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

func TestContentHashInfo(t *testing.T) {
	t.Parallel()

	const sha1Upper = "0123456789ABCDEF0123456789ABCDEF01234567"
	tests := []struct {
		name      string
		hashName  string
		hashValue string
		want      string
	}{
		{name: "implicit SHA1", hashValue: sha1Upper, want: "0123456789abcdef0123456789abcdef01234567"},
		{name: "explicit SHA1", hashName: "SHA-1", hashValue: sha1Upper, want: "0123456789abcdef0123456789abcdef01234567"},
		{name: "MD5 length", hashValue: "0123456789abcdef0123456789abcdef"},
		{name: "explicit different algorithm", hashName: "md5", hashValue: sha1Upper},
		{name: "non hexadecimal", hashName: "sha1", hashValue: "zz23456789abcdef0123456789abcdef01234567"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := contentHashInfo(tt.hashName, tt.hashValue).GetHash(utils.SHA1)
			if got != tt.want {
				t.Fatalf("SHA1 = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFileToObjExposesValidatedSHA1(t *testing.T) {
	t.Parallel()

	const sha1Value = "0123456789abcdef0123456789abcdef01234567"
	obj := fileToObj(File{
		Fid:             "file-id",
		FileName:        "archive.bin",
		FileType:        "1",
		ContentHashName: "sha1",
		ContentHash:     sha1Value,
	})

	if got := obj.GetHash().GetHash(utils.SHA1); got != sha1Value {
		t.Fatalf("object SHA1 = %q, want %q", got, sha1Value)
	}
}
