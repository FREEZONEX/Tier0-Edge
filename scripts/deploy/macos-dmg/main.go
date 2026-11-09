package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/leaanthony/dmg/dsstore"
	"github.com/leaanthony/dmg/hfsplus"
	"github.com/leaanthony/dmg/udif"
)

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: macos-dmg IMAGE_DIR OUTPUT_DMG VOLUME_NAME ALIAS_ASSETS")
		os.Exit(2)
	}
	if err := buildImage(os.Args[1], os.Args[2], os.Args[3], os.Args[4]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func buildImage(source, output, volumeName, assets string) error {
	app, err := imageEntry("Tier0 Edge.app", filepath.Join(source, "Tier0 Edge.app"))
	if err != nil {
		return err
	}
	readme, err := imageEntry("Read Me.txt", filepath.Join(source, "安装说明.txt"))
	if err != nil {
		return err
	}
	background, err := imageEntry(".background", filepath.Join(source, ".background"))
	if err != nil {
		return err
	}
	if len(background.Children) != 1 || background.Children[0].Name != "background.png" {
		return fmt.Errorf("expected .background/background.png")
	}
	alias, err := readApplicationsAlias(assets)
	if err != nil {
		return err
	}
	volume := &hfsplus.Volume{
		Name: volumeName, CreatedAt: time.Now(),
		Children: []*hfsplus.Entry{app, alias.entry, background, readme},
	}
	hfsplus.AssignCNIDs(volume)
	store := dsstore.New()
	store.SetWindowSettings(dsstore.WindowSettings{Top: 100, Left: 180, Bottom: 628, Right: 940})
	const hfsEpochOffset = 2082844800
	date := uint32(volume.CreatedAt.Unix() + hfsEpochOffset)
	store.SetBackgroundImage(volumeName, ".background/background.png",
		&dsstore.IconViewOptions{IconSize: 96, TextSize: 14, GridSpace: 100, LabelOnBottom: true},
		&dsstore.AliasFileInfo{VolCreationDate: date, FileCreationDate: date,
			FileCNID: background.Children[0].CNID, ParentCNID: background.CNID})
	store.SetIconPosition(app.Name, 185, 205)
	store.SetIconPosition(alias.entry.Name, 575, 205)
	store.SetIconPosition(readme.Name, 650, 380)
	var layout bytes.Buffer
	if err := store.WriteTo(&layout); err != nil {
		return err
	}
	volume.Children = append(volume.Children, &hfsplus.Entry{Name: ".DS_Store", Kind: hfsplus.KindFile, Data: layout.Bytes()})
	image, err := hfsplus.Build(volume)
	if err != nil {
		return err
	}
	if err := alias.attachFinderMetadata(image); err != nil {
		return err
	}
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	buildErr := udif.Build(file, image, "whole disk (Apple_HFS : 0)")
	closeErr := file.Close()
	if buildErr != nil {
		return buildErr
	}
	return closeErr
}

func imageEntry(name, source string) (*hfsplus.Entry, error) {
	info, err := os.Lstat(source)
	if err != nil {
		return nil, err
	}
	entry := &hfsplus.Entry{Name: name, Mode: uint16(info.Mode().Perm())}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		entry.Kind = hfsplus.KindSymlink
		entry.Target, err = os.Readlink(source)
	case info.IsDir():
		entry.Kind = hfsplus.KindDir
		children, err := os.ReadDir(source)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			item, err := imageEntry(child.Name(), filepath.Join(source, child.Name()))
			if err != nil {
				return nil, err
			}
			entry.Children = append(entry.Children, item)
		}
	case info.Mode().IsRegular():
		entry.Kind = hfsplus.KindFile
		entry.Data, err = os.ReadFile(source)
	default:
		return nil, fmt.Errorf("unsupported image entry: %s", source)
	}
	return entry, err
}
