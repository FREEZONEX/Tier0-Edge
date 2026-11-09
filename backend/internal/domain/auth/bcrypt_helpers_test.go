package auth

// 测试目标：auth 服务纯函数中的 bcrypt 辅助（测试专用）。
// 说明：避免在多个测试文件中重复 import bcrypt。

import (
	"golang.org/x/crypto/bcrypt"
)

func bcryptGenerate(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}
