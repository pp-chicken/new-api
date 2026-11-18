package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const updateLabel = launchdLabel + ".update"

func updatePlistPath(mainPath string) string {
	return filepath.Join(filepath.Dir(mainPath), updateLabel+".plist")
}

func writeServiceFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".codex-ws-proxy-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func binaryDigest(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return fmt.Appendf(nil, "%x\n", h.Sum(nil)), nil
}

func installUpdater(executable, mainPath, logDir string) error {
	digest, err := binaryDigest(executable)
	if err != nil {
		return err
	}
	path := updatePlistPath(mainPath)
	plist := launchdPlist(updateLabel, []string{executable, "-check-service-update"}, filepath.Join(logDir, "update.out.log"), filepath.Join(logDir, "update.err.log"))
	if err := writeServiceFile(path, []byte(plist)); err != nil {
		return err
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	_ = runLaunchctl("bootout", domain+"/"+updateLabel)
	if err := writeServiceFile(mainPath+".sha256", digest); err != nil {
		return err
	}
	return runLaunchctl("bootstrap", domain, path)
}

func checkServiceUpdate() error {
	path, err := launchAgentPath()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	digest, err := binaryDigest(executable)
	if err != nil {
		return err
	}
	previous, err := os.ReadFile(path + ".sha256")
	if err != nil {
		return err
	}
	if bytes.Equal(digest, previous) {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	_ = runLaunchctl("bootout", domain+"/"+launchdLabel)
	if err := runLaunchctl("bootstrap", domain, path); err != nil {
		return err
	}
	return writeServiceFile(path+".sha256", digest)
}
