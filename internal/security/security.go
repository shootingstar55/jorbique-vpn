package security

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var uuidFormatRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func SecurePath(filePath, baseDir string, mustExist bool) bool {
	baseNorm, err := filepath.Abs(baseDir)
	if err != nil {
		return false
	}
	if _, err := os.Stat(baseNorm); err != nil {
		return false
	}
	pathNorm, err := filepath.Abs(filePath)
	if err != nil {
		pathNorm = filepath.Join(baseDir, filePath)
		pathNorm, _ = filepath.Abs(pathNorm)
	}
	rel, err := filepath.Rel(baseNorm, pathNorm)
	if err != nil {
		return false
	}
	if strings.HasPrefix(rel, "..") || strings.Contains(rel, "..") {
		return false
	}
	if !strings.HasPrefix(strings.ToLower(pathNorm), strings.ToLower(baseNorm)) {
		return false
	}
	if mustExist {
		if _, err := os.Stat(pathNorm); err != nil {
			return false
		}
	}
	return true
}

func ValidUUID(uuid string, requireV4 bool) bool {
	uuid = strings.TrimSpace(uuid)
	if !uuidFormatRe.MatchString(uuid) {
		return false
	}
	parts := strings.Split(uuid, "-")
	if len(parts) != 5 || len(parts[0]) != 8 || len(parts[1]) != 4 ||
		len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		return false
	}
	if requireV4 {
		if len(parts[2]) < 2 {
			return false
		}
		if parts[2][0] != '4' {
			return false
		}
		if !strings.ContainsAny(parts[2][1:2], "89abAB") {
			return false
		}
	}
	return true
}
