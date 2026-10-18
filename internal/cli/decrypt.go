package cli

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	"fmt"
	"unicode/utf8"
)

// Everybody Codes uses hex ciphertext, a UTF-8 AES key, CBC with the first
// 16 bytes of that key as the IV, and PKCS#7 padding.
func decryptNotes(encoded, key string) ([]byte, error) {
	keyBytes := []byte(key)
	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid AES key length: %d bytes", len(keyBytes))
	}
	data, err := hex.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("encoded notes must be hexadecimal")
	}
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errors.New("encoded notes must contain complete AES blocks")
	}
	cipher.NewCBCDecrypter(block, keyBytes[:aes.BlockSize]).CryptBlocks(data, data)
	padding := int(data[len(data)-1])
	if padding < 1 || padding > aes.BlockSize || padding > len(data) {
		return nil, errors.New("invalid PKCS#7 padding")
	}
	for _, value := range data[len(data)-padding:] {
		if int(value) != padding {
			return nil, errors.New("invalid PKCS#7 padding")
		}
	}
	data = data[:len(data)-padding]
	if !utf8.Valid(data) {
		return nil, errors.New("decoded notes are not valid UTF-8")
	}
	return data, nil
}
