//go:build windows

package system

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

func ListDrives() ([]Drive, error) {
	ps := `$ErrorActionPreference='Stop'; Get-CimInstance Win32_LogicalDisk -Filter "DriveType=3 OR DriveType=2" | Select-Object DeviceID,Size,FreeSpace | ConvertTo-Json -Compress`
	out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps).Output()
	if err != nil {
		return nil, fmt.Errorf("could not read Windows drives: %w", err)
	}
	type disk struct {
		DeviceID  string `json:"DeviceID"`
		Size      uint64 `json:"Size"`
		FreeSpace uint64 `json:"FreeSpace"`
	}
	var items []disk
	if err := json.Unmarshal(out, &items); err != nil {
		var one disk
		if e := json.Unmarshal(out, &one); e != nil {
			return nil, fmt.Errorf("parse drive list: %w", err)
		}
		items = []disk{one}
	}
	drives := make([]Drive, 0, len(items))
	for _, d := range items {
		if strings.TrimSpace(d.DeviceID) != "" {
			drives = append(drives, Drive{Name: d.DeviceID, Mount: d.DeviceID + `\`, TotalBytes: d.Size, FreeBytes: d.FreeSpace})
		}
	}
	return drives, nil
}

func PickFolder() (string, error) {
	ps := `Add-Type -AssemblyName System.Windows.Forms; $d=New-Object System.Windows.Forms.FolderBrowserDialog; $d.Description='Choose where UNNAMED PTERO should store this Minecraft server'; $d.ShowNewFolderButton=$true; if($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK){[Console]::Write($d.SelectedPath)}`
	out, err := exec.Command("powershell.exe", "-STA", "-NoProfile", "-NonInteractive", "-Command", ps).Output()
	if err != nil {
		return "", fmt.Errorf("folder picker failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
