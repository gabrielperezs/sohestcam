package googledrive

import (
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"strings"

	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
)

const DirectoryName = "sohestcam.app.videos"
const DirectoryMimeType = "application/vnd.google-apps.folder"

type GoogleDriveClient struct {
	credentials []byte
	client      *http.Client
	srv         *drive.Service
}

func (gdc *GoogleDriveClient) connect() {
	ctx := context.Background()

	// If modifying these scopes, delete your previously saved credentials
	// at ~/.credentials/drive-go-quickstart.json
	config, err := google.ConfigFromJSON(gdc.credentials, drive.DriveScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
		return
	}

	gdc.client = getClient(ctx, config)

	var errDrive error
	gdc.srv, errDrive = drive.New(gdc.client)
	if err != nil {
		log.Fatalf("Unable to retrieve drive Client %v", errDrive)
	}

	log.Printf("Connecetd to google drive")
}

func (gdc *GoogleDriveClient) DirectoryExists(name string, parentId string) (*drive.File, error) {

	path := strings.Split(name, "/")

	for i, d := range path {

		var query string

		if parentId != "" {
			query = fmt.Sprintf("mimeType='%s' and name ='%s' and parents in '%s'", DirectoryMimeType, d, parentId)
		} else {
			query = fmt.Sprintf("mimeType='%s' and name ='%s'", DirectoryMimeType, d)

		}
		r, err := gdc.srv.Files.List().Q(query).PageSize(100).Fields("nextPageToken, files(id, name)").Do()
		if err != nil {
			log.Fatalf("Unable to retrieve files: %v", err)
			return nil, err
		}

		if len(r.Files) > 0 {
			for _, x := range r.Files {
				if i == len(path)-1 {
					return x, nil
				}
				parentId = x.Id
				continue
			}
		} else {
			return nil, nil
		}

	}

	return nil, nil

}

func (gdc *GoogleDriveClient) CreateDirectory(name string) (*drive.File, error) {

	var f *drive.File
	var parent *drive.File

	path := strings.Split(name, "/")

	for _, d := range path {

		if parent == nil {
			log.Println("Checking", d)
		} else {
			log.Println("Checking", d, " - ", parent.Name)
		}

		var err error
		if parent != nil {
			f, err = gdc.DirectoryExists(d, parent.Id)
		} else {
			f, err = gdc.DirectoryExists(d, "")
		}

		if err == nil && f == nil {
			dstFile := &drive.File{
				Name:     d,
				MimeType: DirectoryMimeType,
			}

			if parent != nil {
				dstFile.Parents = []string{parent.Id}
			}

			// Create directory
			var errCreate error
			f, errCreate = gdc.srv.Files.Create(dstFile).Do()
			if errCreate != nil {
				log.Println("Error", err)
			}
		}

		parent = f

	}

	return f, nil

}

func (gdc *GoogleDriveClient) openFile(path string) (*os.File, os.FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to open file: %s", err)
	}

	info, err := f.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("Failed getting file metadata: %s", err)
	}

	return f, info, nil
}

func (gdc *GoogleDriveClient) UploadFile(filename string, parent *drive.File) (*drive.File, error) {

	srcFile, srcFileInfo, err := gdc.openFile(filename)
	if err != nil {
		log.Println("File error: ", err)
		return nil, err
	}

	// Close file on function exit
	defer srcFile.Close()

	// Instantiate empty drive file
	dstFile := &drive.File{}

	// Use provided file name or use filename
	dstFile.Name = filepath.Base(srcFileInfo.Name())

	// Set provided mime type or get type based on file extension
	dstFile.MimeType = mime.TypeByExtension(filepath.Ext(dstFile.Name))

	// Set parent folders
	dstFile.Parents = []string{parent.Id}

	new := gdc.srv.Files.Create(dstFile)
	driveFile, err := new.Media(srcFile).Do()
	if err != nil {
		log.Printf("Got drive.File, err: %#v\n%v\n------", driveFile, err)
	}

	return driveFile, err
}

func Start(b []byte) *GoogleDriveClient {

	gdc := &GoogleDriveClient{
		credentials: b,
	}

	gdc.connect()

	return gdc
}
