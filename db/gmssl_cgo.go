//go:build cgo

package db

/*
#include <stdlib.h>
#include <string.h>
#include <gmssl/sm4.h>
#include <gmssl/sm3.h>
#include <gmssl/rand.h>
*/
import "C"
import (
	"fmt"
	"unsafe"
)

// ── SM4-GCM Encrypt ────────────────────────────────────────────────────

func sm4GcmEncrypt(key, iv, plaintext []byte) ([]byte, error) {
	if len(key) != C.SM4_KEY_SIZE {
		return nil, fmt.Errorf("sm4 key must be %d bytes", C.SM4_KEY_SIZE)
	}

	var ctx C.SM4_GCM_CTX
	ret := C.sm4_gcm_encrypt_init(&ctx,
		(*C.uint8_t)(unsafe.Pointer(&key[0])), C.size_t(len(key)),
		(*C.uint8_t)(unsafe.Pointer(&iv[0])), C.size_t(len(iv)),
		nil, 0, // no AAD
		C.size_t(C.SM4_GCM_DEFAULT_TAG_SIZE))
	if ret != 1 {
		return nil, fmt.Errorf("sm4 gcm encrypt init failed")
	}

	// Update: output buffer needs extra room (inlen + block_size)
	outlen := C.size_t(0)
	out := make([]byte, len(plaintext)+C.SM4_BLOCK_SIZE)
	ret = C.sm4_gcm_encrypt_update(&ctx,
		(*C.uint8_t)(unsafe.Pointer(&plaintext[0])), C.size_t(len(plaintext)),
		(*C.uint8_t)(unsafe.Pointer(&out[0])), &outlen)
	if ret != 1 {
		return nil, fmt.Errorf("sm4 gcm encrypt update failed")
	}
	result := make([]byte, int(outlen))
	copy(result, out[:outlen])

	// Finish: gets the tag appended
	var finallen C.size_t
	finbuf := make([]byte, C.SM4_BLOCK_SIZE+C.SM4_GCM_DEFAULT_TAG_SIZE)
	ret = C.sm4_gcm_encrypt_finish(&ctx,
		(*C.uint8_t)(unsafe.Pointer(&finbuf[0])), &finallen)
	if ret != 1 {
		return nil, fmt.Errorf("sm4 gcm encrypt finish failed")
	}
	result = append(result, finbuf[:int(finallen)]...)
	return result, nil
}

// ── SM4-GCM Decrypt ────────────────────────────────────────────────────

func sm4GcmDecrypt(key, iv, ciphertext []byte) ([]byte, error) {
	if len(key) != C.SM4_KEY_SIZE {
		return nil, fmt.Errorf("sm4 key must be %d bytes", C.SM4_KEY_SIZE)
	}

	var ctx C.SM4_GCM_CTX
	ret := C.sm4_gcm_decrypt_init(&ctx,
		(*C.uint8_t)(unsafe.Pointer(&key[0])), C.size_t(len(key)),
		(*C.uint8_t)(unsafe.Pointer(&iv[0])), C.size_t(len(iv)),
		nil, 0, // no AAD
		C.size_t(C.SM4_GCM_DEFAULT_TAG_SIZE))
	if ret != 1 {
		return nil, fmt.Errorf("sm4 gcm decrypt init failed")
	}

	// Update
	outlen := C.size_t(0)
	out := make([]byte, len(ciphertext)+C.SM4_BLOCK_SIZE)
	ret = C.sm4_gcm_decrypt_update(&ctx,
		(*C.uint8_t)(unsafe.Pointer(&ciphertext[0])), C.size_t(len(ciphertext)),
		(*C.uint8_t)(unsafe.Pointer(&out[0])), &outlen)
	if ret != 1 {
		return nil, fmt.Errorf("sm4 gcm decrypt failed: wrong password or corrupted data")
	}
	result := make([]byte, int(outlen))
	copy(result, out[:outlen])

	// Finish (verifies tag)
	var finallen C.size_t
	finbuf := make([]byte, C.SM4_BLOCK_SIZE)
	ret = C.sm4_gcm_decrypt_finish(&ctx,
		(*C.uint8_t)(unsafe.Pointer(&finbuf[0])), &finallen)
	if ret != 1 {
		return nil, fmt.Errorf("sm4 gcm decrypt finish failed: wrong password or corrupted data")
	}
	result = append(result, finbuf[:int(finallen)]...)
	return result, nil
}

// ── SM3 PBKDF2 ─────────────────────────────────────────────────────────

func sm3Pbkdf2(password string, salt []byte, iter uint, keylen uint) ([]byte, error) {
	out := make([]byte, keylen)
	cPass := C.CString(password)
	defer C.free(unsafe.Pointer(cPass))

	ret := C.sm3_pbkdf2(cPass, C.size_t(len(password)),
		(*C.uint8_t)(unsafe.Pointer(&salt[0])), C.size_t(len(salt)),
		C.size_t(iter), C.size_t(keylen),
		(*C.uint8_t)(unsafe.Pointer(&out[0])))
	if ret != 1 {
		return nil, fmt.Errorf("sm3 pbkdf2 failed")
	}
	return out, nil
}

// ── Random Bytes ────────────────────────────────────────────────────────

func randBytes(length int) ([]byte, error) {
	out := make([]byte, length)
	ret := C.rand_bytes((*C.uint8_t)(unsafe.Pointer(&out[0])), C.size_t(length))
	if ret != 1 {
		return nil, fmt.Errorf("rand_bytes failed")
	}
	return out, nil
}

// ── Constants ───────────────────────────────────────────────────────────

const (
	Sm4KeySize           = C.SM4_KEY_SIZE           // 16
	Sm4BlockSize         = C.SM4_BLOCK_SIZE         // 16
	Sm4GcmDefaultIVSize  = C.SM4_GCM_DEFAULT_IV_SIZE // 12
	Sm4GcmDefaultTagSize = C.SM4_GCM_DEFAULT_TAG_SIZE // 16
	Sm4GcmMaxIVSize      = C.SM4_GCM_MAX_IV_SIZE    // 64

	Sm3Pbkdf2MinIter         = C.SM3_PBKDF2_MIN_ITER          // 10000
	Sm3Pbkdf2MaxIter         = C.SM3_PBKDF2_MAX_ITER          // ~16M
	Sm3Pbkdf2MaxSaltSize     = C.SM3_PBKDF2_MAX_SALT_SIZE     // 64
	Sm3Pbkdf2DefaultSaltSize = C.SM3_PBKDF2_DEFAULT_SALT_SIZE // 8
)
