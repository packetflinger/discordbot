package main

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
)

// processZIP will verify the structure of the zipped files, copy the
// decompressed files to a local git repo, commit and push.
func (f *FileUpload) processZIP(filename string) {
	pm, err := f.session.UserChannelCreate(f.message.Author.ID)
	if err != nil {
		log.Println("error creating direct message channel:", err)
	}

	archive, err := zip.OpenReader(filename)
	if err != nil {
		log.Println("zip open error:", err)
		return
	}
	defer archive.Close()

	filesAdded := 0
	for _, f := range archive.File {
		if hasPrefix(f.Name, []string{"maps/", "models/", "textures/", "env/", "sounds/", "pics/", "players/"}) {
			err = writeZipFileToRepo(f)
			if err != nil {
				log.Println(err)
				continue
			}
			filesAdded++
		}
	}
	if filesAdded > 0 {
		msg := fmt.Sprintf("Added %s, submitted by %s[%s]", f.name, f.message.Author.Username, f.message.Author.ID)
		err = commitAndPush(msg)
		if err != nil {
			log.Println("git error:", err)
			return
		}
		msg = fmt.Sprintf("Files in `%s` have been committed to the our git repo", f.name)
		f.session.ChannelMessageSend(pm.ID, msg)
		log.Printf("%q committed to git repo", f.name)
	} else {
		msg := fmt.Sprintf("`%s` contains an invalid file structure. It should contain top-level folders matching a mod directory:\n", f.name)
		msg += "```\nmaps/...\nmodels/...\ntextures/...\nenv/...\npics/...\n[etc]\n```"
		f.session.ChannelMessageSend(pm.ID, msg)
	}
}

// Write a single file from inside a compressed archive to the local git repo.
func writeZipFileToRepo(zf *zip.File) error {
	fullpath := path.Join(config.GetRepoPath(), zf.Name)
	err := os.MkdirAll(filepath.Dir(fullpath), 0755)
	if err != nil {
		return fmt.Errorf("error creating full path: %v", err)
	}
	fp, err := zf.Open()
	if err != nil {
		return fmt.Errorf("error opening file in zip: %v", err)
	}
	defer fp.Close()

	dstFile, err := os.OpenFile(fullpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("error opening destination file: %v", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, fp); err != nil {
		return fmt.Errorf("error copying file from zip: %v", err)
	}
	return nil
}
