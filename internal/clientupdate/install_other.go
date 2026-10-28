//go:build !windows && !darwin

package clientupdate

import "errors"

func installPackage(path string, _ packageInfo) error {
	return errors.New("используйте системный установщик")
}
