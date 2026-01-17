package validator_test

import (
	"strings"
	"testing"

	"minIODB/pkg/validator"

	"github.com/stretchr/testify/assert"
)

func TestValidateTableName(t *testing.T) {
	v := validator.New()

	tests := []struct {
		name    string
		table   string
		wantErr bool
	}{
		{"有效表名", "users", false},
		{"带数字", "users_2023", false},
		{"下划线开头", "_private", true},
		{"数字开头", "123_users", true},
		{"含空格", "user table", true},
		{"超长", strings.Repeat("a", 129), true},
		{"SQL关键字", "select", true},
		{"SQL关键字", "table", true},
		{"空表名", "", true},
		{"带连字符", "user-data", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateTableName(tt.table)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateID(t *testing.T) {
	v := validator.New()

	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"有效ID", "user123", false},
		{"带连字符", "user-123", false},
		{"带下划线", "user_123", false},
		{"超长", strings.Repeat("a", 256), true},
		{"空ID", "", true},
		{"含空格", "user 123", true},
		{"带特殊字符", "user@123", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateID(tt.id)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateEmail(t *testing.T) {
	v := validator.New()

	tests := []struct {
		name    string
		email   string
		wantErr bool
	}{
		{"有效邮箱", "test@example.com", false},
		{"带点", "user.name@example.com", false},
		{"带加号", "user+test@example.com", false},
		{"空邮箱", "", true},
		{"无效格式", "invalid", true},
		{"缺少@", "testexample.com", true},
		{"缺少域名", "test@", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateEmail(tt.email)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateURL(t *testing.T) {
	v := validator.New()

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"有效HTTP", "http://example.com", false},
		{"有效HTTPS", "https://example.com", false},
		{"带端口", "http://example.com:8080", false},
		{"带路径", "http://example.com/api", false},
		{"空URL", "", true},
		{"无效协议", "ftp://example.com", true},
		{"缺少协议", "example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateURL(tt.url)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSanitizeString(t *testing.T) {
	v := validator.New()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"普通字符串", "hello world", "hello world"},
		{"含控制字符", "hello\x00world", "helloworld"},
		{"含换行", "hello\nworld", "hello\nworld"},
		{"含制表符", "hello\tworld", "hello\tworld"},
		{"含回车", "hello\rworld", "hello\rworld"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := v.SanitizeString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestQuoteIdentifier(t *testing.T) {
	v := validator.New()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"普通标识符", "users", "\"users\""},
		{"带空格", "user table", "\"user table\""},
		{"单个引号", "user\"data", "\"user\"\"data\""},
		{"带连字符", "user-data", "\"user-data\""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := v.QuoteIdentifier(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestQuoteLiteral(t *testing.T) {
	v := validator.New()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"普通字面量", "test", "'test'"},
		{"带单引号", "test's", "'test''s'"},
		{"带控制字符", "test\x00", "'test'"},
		{"带换行", "test\nline", "'test\nline'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := v.QuoteLiteral(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// 测试包级便捷函数
func TestPackageLevelFunctions(t *testing.T) {
	t.Run("ValidateTableName", func(t *testing.T) {
		err := validator.ValidateTableName("users")
		assert.NoError(t, err)

		err = validator.ValidateTableName("123_invalid")
		assert.Error(t, err)
	})

	t.Run("ValidateID", func(t *testing.T) {
		err := validator.ValidateID("user-123")
		assert.NoError(t, err)

		err = validator.ValidateID("")
		assert.Error(t, err)
	})

	t.Run("SanitizeString", func(t *testing.T) {
		result := validator.SanitizeString("test\x00data")
		assert.Equal(t, "testdata", result)
	})

	t.Run("QuoteIdentifier", func(t *testing.T) {
		result := validator.QuoteIdentifier("table_name")
		assert.Equal(t, "\"table_name\"", result)
	})

	t.Run("QuoteLiteral", func(t *testing.T) {
		result := validator.QuoteLiteral("test's")
		assert.Equal(t, "'test''s'", result)
	})
}

func BenchmarkValidateTableName(b *testing.B) {
	v := validator.New()
	for i := 0; i < b.N; i++ {
		v.ValidateTableName("users_table_2023")
	}
}

func BenchmarkValidateID(b *testing.B) {
	v := validator.New()
	for i := 0; i < b.N; i++ {
		v.ValidateID("user-123-abc-456")
	}
}

func BenchmarkSanitizeString(b *testing.B) {
	v := validator.New()
	input := "test\x00data\x01with\x02control\x03chars"
	for i := 0; i < b.N; i++ {
		v.SanitizeString(input)
	}
}
