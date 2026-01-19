package plasmactlplatform

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/launchrctl/launchr"
)

// getRepoInfo returns repository name, version (tag or commit SHA), and error
func getRepoInfo() (repoName, version string, err error) {
	// Open repository
	r, err := git.PlainOpen(".")
	if err != nil {
		return "", "", err
	}

	// Get repository name from remote URL
	remote, err := r.Remote("origin")
	if err != nil {
		return "", "", err
	}
	repoName = remote.Config().URLs[0]
	repoName = filepath.Base(repoName)
	repoName = strings.TrimSuffix(repoName, ".git")

	// Get HEAD reference
	head, err := r.Head()
	if err != nil {
		return "", "", err
	}

	// Check if HEAD points to a tag
	tags, err := r.Tags()
	if err != nil {
		return "", "", err
	}

	var tagName string
	err = tags.ForEach(func(ref *plumbing.Reference) error {
		if ref.Hash() == head.Hash() {
			tagName = ref.Name().Short()
			return fmt.Errorf("found") // Break iteration
		}
		return nil
	})

	if tagName != "" {
		// HEAD is on a tag, use tag name as version
		version = tagName
	} else {
		// Not on a tag, use short commit SHA
		version = head.Hash().String()[:7]
	}

	return repoName, version, nil
}

// createImage creates a Platform Image (.pi) archive
// hasPrepareAction indicates whether platform:prepare action is available
func createImage(hasPrepareAction bool) error {
	// Get repository information
	repoName, version, err := getRepoInfo()
	if err != nil {
		launchr.Log().Error("error", "error", err)
		return fmt.Errorf("error getting repository information: %w", err)
	}

	// Construct image file name: {name}-{version}.pi
	imageFile := fmt.Sprintf("%s-%s.pi", repoName, version)

	// Determine source directory based on prepare action availability
	prepareDir := ".plasma/prepare"
	composeDir := ".plasma/package/compose/merged"
	var srcDir string

	if hasPrepareAction {
		// prepare action exists - must use prepare output for deployable image
		if _, err := os.Stat(prepareDir); os.IsNotExist(err) {
			return fmt.Errorf("platform:prepare action exists but %s not found: run platform:prepare first", prepareDir)
		}
		srcDir = prepareDir
	} else {
		// prepare action doesn't exist - use compose output directly
		if _, err := os.Stat(composeDir); os.IsNotExist(err) {
			return fmt.Errorf("no source directory found: run package:compose first")
		}
		srcDir = composeDir
	}

	// Output to img/ - visible to users as final distributable artifact
	imageTempDir := "img/.tmp"
	imageFinalDir := "img"

	launchr.Term().Printfln("Creating Platform Image %s from %s...", imageFile, srcDir)
	err = createArchive(srcDir, imageTempDir, imageFinalDir, imageFile)
	if err != nil {
		return fmt.Errorf("error creating image: %w", err)
	}

	launchr.Term().Success().Printfln("Platform Image created: %s/%s", imageFinalDir, imageFile)
	return nil
}

func createArchive(srcDir, archiveTempDir, archiveFinalDir, archiveDestFile string) error {
	// Ensure archive directory exists
	if err := os.MkdirAll(archiveTempDir, 0750); err != nil {
		return err
	}
	if err := os.MkdirAll(archiveFinalDir, 0750); err != nil {
		return err
	}

	// Create tar.gz archive
	archivePath := filepath.Join(archiveTempDir, archiveDestFile)
	artifactPath := filepath.Join(archiveFinalDir, archiveDestFile)
	tarFile, err := os.Create(path.Clean(archivePath))
	if err != nil {
		return err
	}
	defer tarFile.Close()

	gw := gzip.NewWriter(tarFile)

	tw := tar.NewWriter(gw)

	err = filepath.Walk(srcDir, func(fpath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Construct the relative path
		relPath, err := filepath.Rel(srcDir, fpath)
		if err != nil {
			return err
		}

		// Create a tar header
		header, err := tar.FileInfoHeader(info, relPath)
		if err != nil {
			return err
		}

		// Modify the name to preserve the directory structure
		header.Name = filepath.ToSlash(relPath)

		// Write the header to the tar archive
		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// If not a directory or symlink, write file content to tar archive
		if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			file, err := os.Open(path.Clean(fpath))
			if err != nil {
				return err
			}
			defer file.Close()

			if _, err := io.Copy(tw, file); err != nil {
				return err
			}
		}

		// If it's a symlink, add it to the archive
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(fpath)
			if err != nil {
				return err
			}

			header.Linkname = link
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("error walking directory: %v", err)
	}

	// Close the tar writer
	if err = tw.Close(); err != nil {
		return fmt.Errorf("error closing tar writer: %v", err)
	}

	// Close the gzip writer
	if err = gw.Close(); err != nil {
		return fmt.Errorf("error closing gzip writer: %v", err)
	}

	// Copy archive to final directory
	srcFile, err := os.Open(path.Clean(archivePath))
	if err != nil {
		return fmt.Errorf("error opening archive file: %v", err)
	}
	defer srcFile.Close()

	destFile, err := os.Create(path.Clean(artifactPath))
	if err != nil {
		return fmt.Errorf("error creating image file: %v", err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return fmt.Errorf("error copying archive to image directory: %v", err)
	}

	// Delete temp file
	if err := os.Remove(path.Clean(archivePath)); err != nil {
		return fmt.Errorf("error deleting temp file: %v", err)
	}

	return nil
}

// listFiles lists files in the specified directory for debugging purposes
func listFiles(dir string) error {
	launchr.Log().Debug("listing files", "directory", dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			launchr.Log().Debug("directory does not exist", "directory", dir)
			return nil
		}
		return fmt.Errorf("error reading directory %s: %w", dir, err)
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		launchr.Log().Debug("file", "name", entry.Name(), "size", info.Size(), "isDir", entry.IsDir())
	}

	return nil
}
