# Docker Cleanup Flow

## Algorithm

Docker images in Artifactory can be 2 types - multiplatform manifest lists (e.g. `infobip-docker-java:2.36.0`) and standalone single-platform images (e.g. `infobip-docker-java:2.35.0`).
Manifest lists reference multiple platform-specific images by their content digests (e.g. `sha256:dd976b...`), getting content of manifest list is updating its downloaded timestamp (we can't rely on this stat).
That digest of underlying images we can get by AQL `manifest.json` (same way as `list.manifest.json`) - artifactory returns "path","created","sha256","stat.downloaded" - these columns are same for manifest list.

So to match manifest list with its underlying images we can do:

1. AQL: `name=list.manifest.json` → get all manifest list tags + created date
   - For each manifest list we can extract image name and version, so we can already do rule matching and make some decisions at this point, later these decisions will be propagated to all underlying paths
2. For each tag: read content to extract digests [sha256:aaa, sha256:bbb]
3. AQL: `name=manifest.json` → get all docker image manifests and their stats. Return a dict with key=sha256 and value=manifest info (path, created, stat.downloaded) so lookup by digest is O(1)
4. For each manifest list digest lookup for underlying manifest info by digest, if found pop the value from dict.
   If no previous decision - we calculate effective downloaded timestamp for manifest list as max(stat.downloaded) based on underlying manifests and make decision based on this timestamp, propagate it to underlying manifests as well as manifest list itself.
5. If manifest list has no underlying manifest found - mark it as orphan (DELETE) as this manifest list is not valid anymore
6. After processing all manifest lists - process rules for all remaining manifest.json in dict (from step 3)
7. For recentVersion rule we have to iterate over decision map again and check versions for each image.
   - can be a case when image can have manifest list and manifest.json, so we have to keep an eye on versions for both and make decision only when we know all versions for this image.
