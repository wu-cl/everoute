package utils

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	coretypes "k8s.io/apimachinery/pkg/types"
)

func EncodeNamespacedName(namespacedName coretypes.NamespacedName) string {
	// encode name and namespace with base64
	var b, temp []byte
	base64.StdEncoding.Encode(temp, []byte(namespacedName.Name))
	b = append(b, temp...)
	base64.StdEncoding.Encode(temp, []byte(namespacedName.Namespace))
	b = append(b, temp...)

	// encode with sha356
	hash := sha256.Sum256(b)

	return fmt.Sprintf("%x", hash)
}
