package boot

import (
	"os"
	"path/filepath"
)

// FixSiblingSingBox chmods bundled sing-box to 0755 if it exists without execute bits (common after unzip).
func FixSiblingSingBox(binDir string) {
	if binDir == "" {
		return
	}
	p := filepath.Join(binDir, "sing-box")
	st, err := os.Lstat(p)
	if err != nil || !st.Mode().IsRegular() {
		return
	}
	if st.Mode().Perm()&0o111 == 0 {
		_ = os.Chmod(p, 0o755)
	}
}
