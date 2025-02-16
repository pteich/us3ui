# Universal S3 UI (us3ui)

Universal S3 UI is a lightweight, cross-platform graphical user interface for managing S3-compatible object storage services, with a particular focus on non-AWS implementations. Built with Go and Fyne, this application provides a seamless experience across macOS, Windows, and Linux.

## Features

- **Universal Compatibility**: Works with any S3-compatible storage service (Minio, Ceph, etc.)
- **Secure Connections**: Support for both HTTP and HTTPS connections
- **File Management**:
    - Browse objects in your bucket with size information
    - Upload local files to your bucket
    - Download objects from your bucket
    - Delete objects with confirmation dialog
    - Refresh bucket contents

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

### Usage

1. Launch the application
2. Enter your S3 service configuration details
3. Click "Connect" to establish the connection
4. Use the main interface to:
    - View objects in your bucket
    - Click "Refresh" to update the object list
    - Select an object and use "Download" or "Delete" buttons
    - Use "Upload" to add new files to your bucket
    - Click "Exit" to close the application

## Technical Details

Built using:
- [Fyne](https://fyne.io/) - Cross-platform GUI toolkit for Go
- [MinIO Go Client](https://github.com/minio/minio-go) - S3-compatible storage client

## Security Note

Ensure you're using SSL (HTTPS) when connecting to production servers to protect your credentials and data in transit.

## License

MIT License