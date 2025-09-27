package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/pbkdf2"
)

const (
	chromePath     = "Library/Application Support/Google/Chrome"
	loginDataFile  = "Login Data"
	defaultProfile = "Default"
)

type PasswordEntry struct {
	Type         string `json:"type"`
	Title        string `json:"title"`
	Subtitle     string `json:"subtitle"`
	Arg          string `json:"arg"`
	Valid        string `json:"valid"`
	Autocomplete string `json:"autocomplete"`
}

type AlfredOutput struct {
	Items []PasswordEntry `json:"items"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: chrome-passwords [query|decrypt] [args...]")
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "query":
		queryPasswords()
	case "decrypt":
		if len(os.Args) < 3 {
			fmt.Println("Usage: chrome-passwords decrypt <encrypted_password>")
			os.Exit(1)
		}
		decryptPassword(os.Args[2])
	default:
		fmt.Println("Unknown command. Use 'query' or 'decrypt'")
		os.Exit(1)
	}
}

func queryPasswords() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal("Failed to get home directory:", err)
	}

	loginDataPath := filepath.Join(homeDir, chromePath, defaultProfile, loginDataFile)

	// 创建临时文件副本，因为Chrome可能正在使用原文件
	tmpFile, err := os.CreateTemp("", "chrome_login_data_*")
	if err != nil {
		log.Fatal("Failed to create temp file:", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// 复制原文件到临时文件
	originalData, err := os.ReadFile(loginDataPath)
	if err != nil {
		log.Fatal("Failed to read login data file:", err)
	}

	if _, err := tmpFile.Write(originalData); err != nil {
		log.Fatal("Failed to write to temp file:", err)
	}
	tmpFile.Close()

	// 打开SQLite数据库
	db, err := sql.Open("sqlite3", tmpFile.Name())
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	// 查询密码数据
	rows, err := db.Query(`
		SELECT origin_url, username_value, password_value 
		FROM logins 
		ORDER BY times_used DESC
	`)
	if err != nil {
		log.Fatal("Failed to query database:", err)
	}
	defer rows.Close()

	var passwords []PasswordEntry
	var query string
	if len(os.Args) > 2 {
		query = strings.Join(os.Args[2:], " ")
	}

	for rows.Next() {
		var originURL, username, passwordValue string
		if err := rows.Scan(&originURL, &username, &passwordValue); err != nil {
			continue
		}

		if len(passwordValue) < 4 {
			continue
		}
		// fmt.Println("pwd length", len(passwordValue))
		// 解码密码（Base64编码加密数据）
		encryptedData := passwordValue[3:] // 跳过前3个字节（版本信息）
		encodedPassword := base64.StdEncoding.EncodeToString([]byte(encryptedData))

		// 解析URL获取域名
		parsedURL, err := url.Parse(originURL)
		if err != nil {
			continue
		}

		title := parsedURL.Hostname()
		if strings.HasPrefix(strings.ToLower(title), "www.") {
			title = title[4:]
		}

		if parsedURL.Scheme == "android" {
			parts := strings.Split(title, "@")
			if len(parts) > 1 {
				title = fmt.Sprintf("%s://%s", parsedURL.Scheme, parts[1])
			}
		}

		entry := PasswordEntry{
			Type:         "default",
			Title:        title,
			Subtitle:     username,
			Arg:          encodedPassword,
			Valid:        "true",
			Autocomplete: title,
		}

		if len(encodedPassword) == 0 {
			entry.Valid = "false"
		}

		passwords = append(passwords, entry)
	}

	// 模糊搜索
	var results []PasswordEntry
	if query == "" {
		results = passwords
	} else {
		// 实现模糊搜索
		results = fuzzySearch(passwords, query)
	}

	// 输出Alfred兼容的JSON
	output := AlfredOutput{Items: results}
	jsonData, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		log.Fatal("Failed to marshal JSON:", err)
	}

	fmt.Println(string(jsonData))
}

func fuzzySearch(entries []PasswordEntry, query string) []PasswordEntry {
	type searchable struct {
		entry PasswordEntry
		text  string
	}

	var searchables []searchable
	for _, entry := range entries {
		searchables = append(searchables, searchable{
			entry: entry,
			text:  fmt.Sprintf("%s %s", entry.Title, entry.Subtitle),
		})
	}

	// 简单的模糊匹配实现
	var results []PasswordEntry
	queryLower := strings.ToLower(query)

	for _, s := range searchables {
		textLower := strings.ToLower(s.text)

		// 简单的包含匹配
		if strings.Contains(textLower, queryLower) {
			results = append(results, s.entry)
			continue
		}

		// 简单的模糊匹配：检查是否所有查询字符都按顺序出现在文本中
		if fuzzyMatch(textLower, queryLower) {
			results = append(results, s.entry)
		}
	}

	return results
}

func fuzzyMatch(text, query string) bool {
	if query == "" {
		return true
	}

	queryIndex := 0
	for i := 0; i < len(text) && queryIndex < len(query); i++ {
		if text[i] == query[queryIndex] {
			queryIndex++
		}
	}

	return queryIndex == len(query)
}

func decryptPassword(encryptedPassword string) {
	// 从Keychain获取Chrome安全存储密码
	cmd := exec.Command("security", "find-generic-password", "-w", "-s", "Chrome Safe Storage", "-a", "Chrome")
	output, err := cmd.Output()
	if err != nil {
		log.Fatal("Failed to get Chrome Safe Storage password:", err)
	}

	masterPassword := strings.TrimSpace(string(output))

	// 解码Base64密码
	decoded, err := base64.StdEncoding.DecodeString(encryptedPassword)
	if err != nil {
		log.Fatal("Failed to decode base64 password:", err)
	}

	// 生成解密密钥
	key := pbkdf2.Key([]byte(masterPassword), []byte("saltysalt"), 1003, 16, sha1.New)

	// 创建AES解密器
	block, err := aes.NewCipher(key)
	if err != nil {
		log.Fatal("Failed to create cipher:", err)
	}

	// 使用CBC模式解密
	iv := []byte("                ") // 16个空格
	mode := cipher.NewCBCDecrypter(block, iv)

	// 确保数据长度是块大小的倍数
	paddedData := make([]byte, len(decoded))
	copy(paddedData, decoded)
	if len(paddedData)%aes.BlockSize != 0 {
		// 填充到块大小
		padLen := aes.BlockSize - (len(paddedData) % aes.BlockSize)
		padding := make([]byte, padLen)
		paddedData = append(paddedData, padding...)
	}

	mode.CryptBlocks(paddedData, paddedData)

	// 移除填充和不可打印字符
	decrypted := make([]rune, 0)
	for _, r := range string(paddedData) {
		if unicode.IsPrint(r) && r != 0 {
			decrypted = append(decrypted, r)
		}
	}

	fmt.Print(string(decrypted))
}
