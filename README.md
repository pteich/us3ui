# Universal S3 UI (us3ui)

Universal S3 UI is a lightweight, cross-platform graphical user interface for managing S3-compatible object storage services, with a particular focus on non-AWS implementations. 
Built with Go and Fyne, this application provides a seamless experience across macOS, Windows, and Linux and is provided as a native app.

## Features

- **Universal Compatibility**: Works with any S3-compatible storage service (Minio, Ceph, etc.)
- **Secure Connections**: Support for both HTTP and HTTPS connections
- **File Management**:
    - Browse objects in your bucket with size information
    - Upload local files to your bucket
    - Download selected files from your bucket to your local machine
    - Delete objects
    - Select files and generate temporary download links valid for one hour
    - Refresh bucket contents
- **Asynchronous Loading**: Load objects without blocking the UI
- **Search Functionality**: Easily find objects with simple fulltext search
- **Progress Tracking**: Visual progress bar for long-running operations
- **Pagination**: Load objects in batches for improved performance
- **Detailed Object Information**: View object name, size, and last modified date
- **Responsive UI**: Resizable columns for better visibility of object details

## Planned Features

- *Connection Manager*: Save and manage multiple S3 service configurations
- *Drag-and-Drop Support*: Drag multiple files from your local machine to upload to your bucket
- *Sorting*: Sort objects by name, size, or last modified date

## Download Pre-built Binaries

You can download pre-built binaries for Linux, macOS, and Windows from the [GitHub Releases](https://github.com/pteich/us3ui/releases) page. This allows you to quickly get started with Universal S3 UI without needing to build from source. Simply download the appropriate binary for your operating system, extract it, and run the executable.

## Getting Started

### Prerequisites

- Any S3-compatible storage service credentials
- For building from source: Go programming language

### Configuration

When you start the application, you'll need to provide:
- **Endpoint**: Your S3 service endpoint (e.g., "play.min.io")
- **Access Key**: Your S3 access key
- **Secret Key**: Your S3 secret key
- **Bucket Name**: The name of the bucket you want to access
- **SSL**: Toggle for HTTPS connection (recommended for production use)

The configuration values can also be preset using CLI flags or environment variables. The available options are:

- **Endpoint**: `--endpoint` or `ENDPOINT`
- **Access Key**: `--accesskey` or `ACCESS_KEY`
- **Secret Key**: `--secretkey` or `SECRET_KEY`
- **Bucket Name**: `--bucket` or `BUCKET`
- **SSL**: `--usessl` or `USE_SSL`

### Usage

1. Launch the application
2. Enter your S3 service configuration details
3. Click "Connect" to establish the connection
4. Use the main interface to:
    - View objects in your bucket with detailed information
    - Use the search bar to find specific objects
    - Select one or multiple objects to download or delete files
    - Use upload to add new local files to your bucket
    - Select files and generate temporary download links
    - Click "Exit" to close the application

## Screenshots

Here's a visual walkthrough of the application:

### Login Screen
![Login Screen](screenshots/login.png)
*The login screen where you enter your S3 service configuration details*

### File List View
![File List](screenshots/filelist.png)
*Main interface showing the list of objects in your bucket*

### Select files to download
![File List](screenshots/selectfiles.png)
*Select one or multiple objects by clicking the checkbox in the first column to download or delete*

### Upload Dialog
![Upload Dialog](screenshots/upload-file.png)
*File upload interface for adding new objects to your bucket*

## Technical Details

Built using:
- [Fyne](https://fyne.io/) - Cross-platform GUI toolkit for Go
- [MinIO Go Client](https://github.com/minio/minio-go) - S3-compatible storage client

## Security Note

Ensure you're using SSL (HTTPS) when connecting to production servers to protect your credentials and data in transit.
All credentials are only used on your local machine to create the connection.

## License

MIT License