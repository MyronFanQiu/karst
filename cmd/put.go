package cmd

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	. "karst/config"
	"karst/logger"
	"karst/merkletree"
	"karst/tee"
	"karst/util"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/cheggaaa/pb"
	"github.com/spf13/cobra"
	"github.com/syndtr/goleveldb/leveldb"
)

func init() {
	putCmd.Flags().String("chain_account", "", "file will be saved in the karst node with this 'chain_account' by storage market")
	rootCmd.AddCommand(putCmd)
}

var putCmd = &cobra.Command{
	Use:   "put [file-path] [flags]",
	Short: "Put file into karst",
	Long:  "A file storage interface provided by karst",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		// Base class
		timeStart := time.Now()
		ReadConfig()

		db, err := leveldb.OpenFile(Config.KarstPaths.DbPath, nil)
		if err != nil {
			logger.Error("Fatal error in opening db: %s\n", err)
			panic(err)
		}
		defer db.Close()
		putProcesser := newPutProcesser(args[0], db)

		// Remote mode or local mode
		chainAccount, _ := cmd.Flags().GetString("chain_account")
		if chainAccount != "" {
			logger.Info("Remote mode, chain account: %s", chainAccount)

			if err := putProcesser.split(true); err != nil {
				putProcesser.dealErrorForRemote(err)
				return
			} else {
				merkleTreeBytes, _ := json.Marshal(putProcesser.MekleTree)
				logger.Debug("Splited merkleTree is %s", string(merkleTreeBytes))
			}

			logger.Info("Remotely put '%s' successfully in %s !", args[0], time.Since(timeStart))
		} else {
			logger.Info("Local mode")

			// Split file
			if err := putProcesser.split(false); err != nil {
				putProcesser.dealErrorForLocal(err)
				return
			} else {
				merkleTreeBytes, _ := json.Marshal(putProcesser.MekleTree)
				logger.Debug("Splited merkleTree is %s", string(merkleTreeBytes))
			}

			// TODO: local put use reserve seal interface of TEE
			// Seal file
			if err := putProcesser.sealFile(); err != nil {
				putProcesser.dealErrorForLocal(err)
				return
			} else {
				merkleTreeSealedBytes, _ := json.Marshal(putProcesser.MekleTreeSealed)
				logger.Debug("Sealed merkleTree is %s", string(merkleTreeSealedBytes))
			}

			// Log results
			logger.Info("Locally put '%s' successfully in %s ! It root hash is '%s' -> '%s'.", args[0], time.Since(timeStart), putProcesser.MekleTree.Hash, putProcesser.MekleTreeSealed.Hash)
		}
	},
}

type PutInfo struct {
	InputfilePath   string
	Md5             string
	MekleTree       *merkletree.MerkleTreeNode
	MekleTreeSealed *merkletree.MerkleTreeNode
	StoredPath      string
}

type PutProcesser struct {
	InputfilePath             string
	Db                        *leveldb.DB
	FileStorePathInBegin      string
	Md5                       string
	FileStorePathInHash       string
	MekleTree                 *merkletree.MerkleTreeNode
	FileStorePathInSealedHash string
	MekleTreeSealed           *merkletree.MerkleTreeNode
}

func newPutProcesser(inputfilePath string, db *leveldb.DB) *PutProcesser {
	return &PutProcesser{
		InputfilePath: inputfilePath,
		Db:            db,
	}
}

// Locally split, duplicate files are not allowed; Remotely split, duplicate files not allowed
func (putProcesser *PutProcesser) split(isRemote bool) error {
	// Open file
	file, err := os.Open(putProcesser.InputfilePath)
	if err != nil {
		return fmt.Errorf("Fatal error in opening '%s': %s", putProcesser.InputfilePath, err)
	}
	defer file.Close()

	fileBasePath := ""
	if isRemote {
		fileBasePath = Config.KarstPaths.TempFilesPath
		// Create md5 file directory
		fileStorePathInBegin := filepath.FromSlash(fileBasePath + "/" + strconv.FormatInt(time.Now().UnixNano(), 10))
		if err := os.MkdirAll(fileStorePathInBegin, os.ModePerm); err != nil {
			return fmt.Errorf("Fatal error in creating file store directory: %s", err)
		} else {
			putProcesser.FileStorePathInBegin = fileStorePathInBegin
		}

	} else {
		fileBasePath = Config.KarstPaths.FilesPath
		// Check md5
		md5hash := md5.New()
		if _, err = io.Copy(md5hash, file); err != nil {
			return fmt.Errorf("Fatal error in calculating md5 of '%s': %s", putProcesser.InputfilePath, err)
		}
		md5hashString := hex.EncodeToString(md5hash.Sum(nil))

		if ok, _ := putProcesser.Db.Has([]byte(md5hashString), nil); ok {
			return fmt.Errorf("This '%s' has already been stored, file md5 is: %s", putProcesser.InputfilePath, md5hashString)
		}

		// Create md5 file directory
		fileStorePathInMd5 := filepath.FromSlash(fileBasePath + "/" + md5hashString + "_" + string(time.Now().UnixNano()))
		if err := os.MkdirAll(fileStorePathInMd5, os.ModePerm); err != nil {
			return fmt.Errorf("Fatal error in creating file store directory: %s", err)
		} else {
			putProcesser.FileStorePathInBegin = fileStorePathInMd5
		}

		// Save md5 into database
		if err = putProcesser.Db.Put([]byte(md5hashString), nil, nil); err != nil {
			return fmt.Errorf("Fatal error in putting information into leveldb: %s", err)
		} else {
			putProcesser.Md5 = md5hashString
		}

		// Go back to file beginning and get file info
		if _, err = file.Seek(0, 0); err != nil {
			return fmt.Errorf("Fatal error in seek file '%s': %s", putProcesser.InputfilePath, err)
		}
	}

	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("Fatal error in getting '%s' information: %s", putProcesser.InputfilePath, err)
	}

	// Split file
	totalPartsNum := uint64(math.Ceil(float64(fileInfo.Size()) / float64(Config.FilePartSize)))
	partHashs := make([][32]byte, 0)
	partSizes := make([]uint64, 0)

	logger.Info("Splitting '%s' to %d parts.", putProcesser.InputfilePath, totalPartsNum)
	bar := pb.StartNew(int(totalPartsNum))
	for i := uint64(0); i < totalPartsNum; i++ {
		// Bar
		bar.Increment()

		// Get part of file
		partSize := int(math.Min(float64(Config.FilePartSize), float64(fileInfo.Size()-int64(i*Config.FilePartSize))))
		partBuffer := make([]byte, partSize)

		if _, err = file.Read(partBuffer); err != nil {
			return fmt.Errorf("Fatal error in getting part of '%s': %s", putProcesser.InputfilePath, err)
		}

		// Get part information
		partHash := sha256.Sum256(partBuffer)
		partHashs = append(partHashs, partHash)
		partSizes = append(partSizes, uint64(partSize))
		partHashString := hex.EncodeToString(partHash[:])
		partFileName := filepath.FromSlash(putProcesser.FileStorePathInBegin + "/" + strconv.FormatUint(i, 10) + "_" + partHashString)

		// Write to disk
		partFile, err := os.Create(partFileName)
		if err != nil {
			return fmt.Errorf("Fatal error in creating the part '%s' of '%s': %s", partFileName, putProcesser.InputfilePath, err)
		}
		partFile.Close()

		if err = ioutil.WriteFile(partFileName, partBuffer, os.ModeAppend); err != nil {
			return fmt.Errorf("Fatal error in writing the part '%s' of '%s': %s", partFileName, putProcesser.InputfilePath, err)
		}
	}
	bar.Finish()

	// Rename folder
	fileMerkleTree := merkletree.CreateMerkleTree(partHashs, partSizes)
	fileStorePathInHash := filepath.FromSlash(fileBasePath + "/" + fileMerkleTree.Hash)

	if !util.IsDirOrFileExist(fileStorePathInHash) {
		if err = os.Rename(putProcesser.FileStorePathInBegin, fileStorePathInHash); err != nil {
			return fmt.Errorf("Fatal error in renaming '%s' to '%s': %s", putProcesser.FileStorePathInBegin, fileStorePathInHash, err)
		} else {
			putProcesser.FileStorePathInHash = fileStorePathInHash
		}
	} else {
		putProcesser.FileStorePathInHash = fileStorePathInHash
		os.RemoveAll(putProcesser.FileStorePathInBegin)
	}

	if !isRemote {
		if err = putProcesser.Db.Put([]byte(fileMerkleTree.Hash), nil, nil); err != nil {
			return fmt.Errorf("Fatal error in putting information into leveldb: %s", err)
		} else {
			putProcesser.MekleTree = fileMerkleTree
		}
	} else {
		putProcesser.MekleTree = fileMerkleTree
	}

	return nil
}

func (putProcesser *PutProcesser) sealFile() error {
	// New TEE
	tee, err := tee.NewTee(Config.TeeBaseUrl, Config.Backup)
	if err != nil {
		return fmt.Errorf("Fatal error in creating tee structure: %s", err)
	}

	// Send merkle tree to TEE for sealing
	mekleTreeSealed, fileStorePathInSealedHash, err := tee.Seal(putProcesser.FileStorePathInHash, putProcesser.MekleTree)
	if err != nil {
		return fmt.Errorf("Fatal error in sealing file '%s' : %s", putProcesser.MekleTree.Hash, err)
	} else {
		putProcesser.FileStorePathInSealedHash = fileStorePathInSealedHash
	}

	// Store sealed merkle tree info to db
	if err = putProcesser.Db.Put([]byte(putProcesser.MekleTree.Hash), []byte(mekleTreeSealed.Hash), nil); err != nil {
		return fmt.Errorf("Fatal error in putting information into leveldb: %s", err)
	} else {
		putInfo := &PutInfo{
			InputfilePath:   putProcesser.InputfilePath,
			Md5:             putProcesser.Md5,
			MekleTree:       putProcesser.MekleTree,
			MekleTreeSealed: mekleTreeSealed,
			StoredPath:      putProcesser.FileStorePathInSealedHash,
		}

		putInfoBytes, _ := json.Marshal(putInfo)
		if err = putProcesser.Db.Put([]byte(mekleTreeSealed.Hash), putInfoBytes, nil); err != nil {
			return fmt.Errorf("Fatal error in putting information into leveldb: %s", err)
		} else {
			putProcesser.MekleTreeSealed = mekleTreeSealed
		}
	}

	return nil
}

func (putProcesser *PutProcesser) dealErrorForRemote(err error) {
	if putProcesser.FileStorePathInBegin != "" {
		os.RemoveAll(putProcesser.FileStorePathInBegin)
	}

	if putProcesser.FileStorePathInHash != "" {
		os.RemoveAll(putProcesser.FileStorePathInHash)
	}

	logger.Error("%s", err)
}

func (putProcesser *PutProcesser) dealErrorForLocal(err error) {
	if putProcesser.FileStorePathInBegin != "" {
		os.RemoveAll(putProcesser.FileStorePathInBegin)
	}

	if putProcesser.FileStorePathInHash != "" {
		os.RemoveAll(putProcesser.FileStorePathInHash)
	}

	if putProcesser.FileStorePathInSealedHash != "" {
		os.RemoveAll(putProcesser.FileStorePathInSealedHash)
	}

	if putProcesser.Md5 != "" {
		if err := putProcesser.Db.Delete([]byte(putProcesser.Md5), nil); err != nil {
			logger.Error("%s", err)
		}
	}

	if putProcesser.MekleTree != nil {
		if err := putProcesser.Db.Delete([]byte(putProcesser.MekleTree.Hash), nil); err != nil {
			logger.Error("%s", err)
		}
	}

	if putProcesser.MekleTreeSealed != nil {
		if err := putProcesser.Db.Delete([]byte(putProcesser.MekleTreeSealed.Hash), nil); err != nil {
			logger.Error("%s", err)
		}
	}

	logger.Error("%s", err)
}
