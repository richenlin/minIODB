package metadata

import (
	"bytes"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	// 测试 AES-256-GCM 加密/解密
	bm := &BackupManager{
		config: BackupConfig{
			EncryptionEnabled: true,
			EncryptionKey:     bytes.Repeat([]byte("a"), 32), // 32字节密钥
		},
	}

	testCases := []struct {
		name      string
		plaintext []byte
	}{
		{"simple text", []byte("Hello, World!")},
		{"empty data", []byte{}},
		{"binary data", []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD}},
		{"large data", bytes.Repeat([]byte("x"), 10000)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 加密
			ciphertext, err := bm.encrypt(tc.plaintext)
			if err != nil {
				t.Fatalf("encrypt failed: %v", err)
			}

			// 验证加密后的数据与原始数据不同（非空数据）
			if len(tc.plaintext) > 0 && bytes.Equal(tc.plaintext, ciphertext) {
				t.Error("ciphertext should be different from plaintext")
			}

			// 解密
			decrypted, err := bm.decrypt(ciphertext)
			if err != nil {
				t.Fatalf("decrypt failed: %v", err)
			}

			// 验证解密后的数据与原始数据相同
			if !bytes.Equal(tc.plaintext, decrypted) {
				t.Errorf("decrypted data mismatch: got %v, want %v", decrypted, tc.plaintext)
			}
		})
	}
}

func TestEncryptDisabled(t *testing.T) {
	// 测试加密禁用时，数据应该原样返回
	bm := &BackupManager{
		config: BackupConfig{
			EncryptionEnabled: false,
			EncryptionKey:     nil,
		},
	}

	plaintext := []byte("test data")

	// encrypt 应该原样返回
	encrypted, err := bm.encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	if !bytes.Equal(plaintext, encrypted) {
		t.Error("encrypted data should be same as plaintext when encryption disabled")
	}

	// decrypt 应该原样返回
	decrypted, err := bm.decrypt(plaintext)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if !bytes.Equal(plaintext, decrypted) {
		t.Error("decrypted data should be same as ciphertext when encryption disabled")
	}
}

func TestInvalidKeyLength(t *testing.T) {
	// 测试无效密钥长度
	bm := &BackupManager{
		config: BackupConfig{
			EncryptionEnabled: true,
			EncryptionKey:     []byte("short"), // 无效密钥长度
		},
	}

	plaintext := []byte("test data")

	// 加密应该失败
	_, err := bm.encrypt(plaintext)
	if err == nil {
		t.Error("encrypt should fail with invalid key length")
	}

	// 解密应该失败
	_, err = bm.decrypt(plaintext)
	if err == nil {
		t.Error("decrypt should fail with invalid key length")
	}
}

func TestCiphertextTooShort(t *testing.T) {
	// 测试密文太短的情况
	bm := &BackupManager{
		config: BackupConfig{
			EncryptionEnabled: true,
			EncryptionKey:     bytes.Repeat([]byte("a"), 32),
		},
	}

	// 密文太短（少于 nonce 大小）
	shortCiphertext := []byte("short")

	_, err := bm.decrypt(shortCiphertext)
	if err == nil {
		t.Error("decrypt should fail with ciphertext too short")
	}
}

func TestRecoveryManagerDecrypt(t *testing.T) {
	// 测试 RecoveryManager 的解密功能
	rm := &RecoveryManager{
		encryptionEnabled: true,
		encryptionKey:     bytes.Repeat([]byte("b"), 32),
		logger:            nil, // 需要日志但测试中可以为 nil
	}

	plaintext := []byte("recovery test data")

	// 使用 BackupManager 加密（使用相同密钥）
	bm := &BackupManager{
		config: BackupConfig{
			EncryptionEnabled: true,
			EncryptionKey:     bytes.Repeat([]byte("b"), 32),
		},
	}

	ciphertext, err := bm.encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	// 使用 RecoveryManager 解密
	decrypted, err := rm.decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("decrypted data mismatch: got %v, want %v", decrypted, plaintext)
	}
}
