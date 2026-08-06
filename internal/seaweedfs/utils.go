package seaweedfs

// splitObjectKey splits "images/proj/2026/07/ulid.jpg" into dir "/images/proj/2026/07" and name "ulid.jpg".
// Filer gRPC DeleteEntry requires the directory with a leading slash and the filename separately.
func splitObjectKey(objectKey string) (dir, name string) {
	for i := len(objectKey) - 1; i >= 0; i-- {
		if objectKey[i] == '/' {
			return "/" + objectKey[:i], objectKey[i+1:]
		}
	}
	return "/", objectKey
}