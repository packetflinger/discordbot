package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"

	"github.com/bwmarrin/discordgo"
	"github.com/packetflinger/libq2/bsp"
	"github.com/packetflinger/libq2/pak"
	"github.com/packetflinger/libq2/proto"
)

type FileUpload struct {
	name      string // the original filename uploaded (no path)
	localName string // temp name in local filesystem
	session   *discordgo.Session
	message   *discordgo.MessageCreate
}

// Download the standalone .bps map file, add it to the repo in the /maps
// directory.
func (f *FileUpload) processBSP(mapfile string) {
	var msg string
	pm, err := f.session.UserChannelCreate(f.message.Author.ID)
	if err != nil {
		log.Println("error creating direct message channel:", err)
	}
	bspfile, err := bsp.OpenBSPFile(mapfile)
	if err != nil {
		msg = fmt.Sprintf("invalid BPS file: %v\n", err)
		log.Println(msg)
		f.session.ChannelMessageSend(pm.ID, msg)
		return
	}
	data, err := os.ReadFile(f.localName)
	if err != nil {
		log.Printf("unable to open %q [%q], aborting\n", f.localName, f.name)
		return
	}
	outname := path.Join(config.RepoPath, "maps", f.name)
	err = os.WriteFile(outname, data, 0644)
	if err != nil {
		log.Printf("unable to write %q to %q, aborting\n", f.localName, outname)
		return
	}
	msg = fmt.Sprintf("Added %s, submitted by %s[%s]", f.name, f.message.Author.Username, f.message.Author.ID)
	err = commitAndPush(msg)
	if err != nil {
		log.Println("git error:", err)
		return
	}
	msg = fmt.Sprintf("Added `%s`\n```  %d bytes\n  %d entities\n  %d textures\n```", f.name, len(data), len(bspfile.Ents), len(bspfile.FetchTextures()))
	f.session.ChannelMessageSend(pm.ID, msg)
}

// If we're given a .pak file to add, it must contain the proper virtual file
// system structure. It should contain a `/maps` folder containing any .bps
// files, a `/textures` directory for textures, etc. Random files in the root
// will not be committed.
func (f *FileUpload) processPAK(filename string) {
	pm, err := f.session.UserChannelCreate(f.message.Author.ID)
	if err != nil {
		log.Println("error creating direct message channel:", err)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		log.Printf("error reading pak data: %v\n", err)
		msg := "sorry, I was unable to process " + filename
		f.session.ChannelMessageSend(pm.ID, msg)
		return
	}
	pakfile, err := pak.Unmarshal(data)
	if err != nil {
		log.Printf("error unmarshalling pak data: %v\n", err)
		msg := fmt.Sprintf("%q invalid pak file", filename)
		f.session.ChannelMessageSend(pm.ID, msg)
		return
	}
	filesAdded := 0
	for _, f := range pakfile.GetFiles() {
		if hasPrefix(f.GetName(), []string{"maps/", "models/", "textures/", "env/", "sounds/", "pics/", "players/"}) {
			err = writePakFileToRepo(f)
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
	}
}

// Write a file from a PAK archive to the local git repo.
func writePakFileToRepo(f *proto.PAKFile) error {
	fullpath := path.Join(config.GetRepoPath(), f.Name)
	err := os.MkdirAll(filepath.Dir(fullpath), 0755)
	if err != nil {
		return fmt.Errorf("error creating full path: %v", err)
	}
	err = os.WriteFile(fullpath, f.Data, 0644)
	if err != nil {
		return fmt.Errorf("error writing %q to repo: %v", fullpath, err)
	}
	return nil
}

// Add any new files in the repo to be tracked by git, then commit and upload.
func commitAndPush(msg string) error {
	git := NewGit(config.RepoPath)
	err := git.add()
	if err != nil {
		log.Println(err)
		return err
	}
	err = git.commit(msg)
	if err != nil {
		log.Println(err)
		return err
	}
	err = git.Push()
	if err != nil {
		log.Println(err)
		return err
	}
	return nil
}
