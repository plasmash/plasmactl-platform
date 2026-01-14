package plasmactlplatform

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/launchrctl/launchr"
)

func getRepoInfo() (repoName, lastCommitMessage, lastCommitShortSHA string, err error) {
	// Open repository
	r, err := git.PlainOpen(".")
	if err != nil {
		return "", "", "", err
	}

	// Get repository name
	remote, err := r.Remote("origin")
	if err != nil {
		return "", "", "", err
	}
	repoName = remote.Config().URLs[0]
	repoName = filepath.Base(repoName)
	repoName = repoName[:len(repoName)-4]

	// Get last commit information
	ref, err := r.Head()
	if err != nil {
		return "", "", "", err
	}
	commit, err := r.CommitObject(ref.Hash())
	if err != nil {
		return "", "", "", err
	}
	lastCommitMessage = commit.Message
	lastCommitShortSHA = ref.Hash().String()[:7]

	return repoName, lastCommitMessage, lastCommitShortSHA, nil
}

// createArtifact creates a tar.gz archive of the current repository for publishing
func createArtifact() error {
	// Get repository information
	repoName, _, lastCommitShortSHA, err := getRepoInfo()
	if err != nil {
		launchr.Log().Error("error", "error", err)
		return fmt.Errorf("error getting repository information: %w", err)
	}

	// Construct artifact file name
	archiveFile := fmt.Sprintf("%s-%s-plasma-src.tar.gz", repoName, lastCommitShortSHA)

	// Variables - use .plasma/platform/package structure
	srcDir := "."
	archiveTempDir := ".plasma/platform/package/tmp"
	archiveFinalDir := ".plasma/platform/package/artifacts"

	launchr.Term().Printfln("Creating artifact %s...", archiveFile)
	err = createArchive(srcDir, archiveTempDir, archiveFinalDir, archiveFile)
	if err != nil {
		return fmt.Errorf("error creating archive: %w", err)
	}

	launchr.Term().Success().Printfln("Artifact created: %s/%s", archiveFinalDir, archiveFile)
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

	excludeDirs := map[string]bool{
		".git":       true,
		".compose":   true,
		".plasma":    true,
		".plasmactl": true,
	}

	err = filepath.Walk(srcDir, func(fpath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip excluded directories
		if excludeDirs[info.Name()] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
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

	// Copy archive to artifact directory
	srcFile, err := os.Open(path.Clean(archivePath))
	if err != nil {
		return fmt.Errorf("error opening archive file: %v", err)
	}
	defer srcFile.Close()

	destFile, err := os.Create(path.Clean(artifactPath))
	if err != nil {
		return fmt.Errorf("error creating artifact file: %v", err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return fmt.Errorf("error copying archive to artifact directory: %v", err)
	}

	// Delete temp file from archive temp directory.
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
