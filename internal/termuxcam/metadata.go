package main

import (
	"fmt"
	"os/exec"
	"time"
)

func AddExifMetadata(filePath string) error {
	if _, err := exec.LookPath("exiftool"); err == nil {
		cmd := exec.Command("exiftool",
			"-overwrite_original",
			fmt.Sprintf("-DateTime=%s", time.Now().Format("2006:01:02 15:04:05")),
			fmt.Sprintf("-Description=TermuxCam %s", time.Now().Format("02/01/2006 15:04:05")),
			"-Make=Termux",
			"-Model=Android",
			filePath,
		)
		return cmd.Run()
	}

	if _, err := exec.LookPath("jhead"); err == nil {
		cmd := exec.Command("jhead",
			fmt.Sprintf("-ts%s", time.Now().Format("2006:01:02_15:04:05")),
			filePath,
		)
		return cmd.Run()
	}

	return nil
}
