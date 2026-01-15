#!/bin/bash

readonly INITIAL_TAG="0.0.0"
readonly IMAGE_DIR="img"

# Function to compare two semver versions
# Returns 1 if $1 > $2, 0 if $1 == $2, -1 if $1 < $2
semver_compare() {
  local IFS=.
  local i ver1=($1) ver2=($2)
  # Append zeroes to make sure both versions have the same length
  for ((i=${#ver1[@]}; i<${#ver2[@]}; i++)); do
    ver1[i]=0
  done
  for ((i=${#ver2[@]}; i<${#ver1[@]}; i++)); do
    ver2[i]=0
  done
  for ((i=0; i<${#ver1[@]}; i++)); do
    if [[ ${ver1[i]} =~ [^0-9] || ${ver2[i]} =~ [^0-9] ]]; then
      if [[ ${ver1[i]} < ${ver2[i]} ]]; then
        echo -1
        return
      elif [[ ${ver1[i]} > ${ver2[i]} ]]; then
        echo 1
        return
      fi
    else
      if ((10#${ver1[i]} > 10#${ver2[i]})); then
        echo 1
        return
      elif ((10#${ver1[i]} < 10#${ver2[i]})); then
        echo -1
        return
      fi
    fi
  done
  echo 0
}

# Check if remote 'origin' exists
remote_exists() {
  git remote get-url origin >/dev/null 2>&1
}

# Function to ensure latest tags are available from remote
ensure_latest_tags() {
  if remote_exists; then
    echo "Fetching latest tags from remote..." >&2
    if git fetch --tags origin 2>/dev/null; then
      echo "Tags synchronized with remote." >&2
      return 0
    else
      echo "Warning: Failed to fetch tags from remote." >&2
      return 1
    fi
  else
    echo "Warning: No remote found." >&2
    return 1
  fi
}

# Get local tags and filter for SemVer
get_local_tags() {
  if ! tags=$(git tag -l 2>/dev/null); then
    echo "Error: Could not access local tags" >&2
    echo ""
    return 1
  fi
  filtered_tags=$(echo "$tags" | grep -E '^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$')
  echo "$filtered_tags"
}

semver_get_latest() {
  # Get local tags and filter for SemVer
  filtered_tags=$(get_local_tags)
  if [[ $? -ne 0 ]]; then
    return 1
  fi

  # Initialize highest version variable
  highest_version=""

  for tag in $filtered_tags; do
    # Remove leading 'v' if present
    version=${tag}
    if [ -z "$highest_version" ]; then
      highest_version=$version
    else
      comparison=$(semver_compare "$version" "$highest_version")
      if [ "$comparison" -eq 1 ]; then
        highest_version=$version
      fi
    fi
  done

  if [ -z "$highest_version" ]; then
    echo ""
  else
    echo "$highest_version"
  fi
}

# Extract host from git remote URL
get_remote_host() {
  local remote_url
  remote_url=$(git remote get-url origin 2>/dev/null)

  # Handle SSH format: git@host:owner/repo.git
  if [[ "$remote_url" =~ ^git@([^:]+): ]]; then
    echo "${BASH_REMATCH[1]}"
    return
  fi

  # Handle HTTPS format: https://host/owner/repo.git
  if [[ "$remote_url" =~ ^https?://([^/]+)/ ]]; then
    echo "${BASH_REMATCH[1]}"
    return
  fi

  echo ""
}

# Extract owner/repo from git remote URL
get_remote_repo() {
  local remote_url
  remote_url=$(git remote get-url origin 2>/dev/null)

  # Handle SSH format: git@host:owner/repo.git
  if [[ "$remote_url" =~ ^git@[^:]+:(.+)\.git$ ]]; then
    echo "${BASH_REMATCH[1]}"
    return
  fi

  # Handle HTTPS format: https://host/owner/repo.git
  if [[ "$remote_url" =~ ^https?://[^/]+/(.+)\.git$ ]]; then
    echo "${BASH_REMATCH[1]}"
    return
  fi

  # Without .git suffix
  if [[ "$remote_url" =~ ^git@[^:]+:(.+)$ ]]; then
    echo "${BASH_REMATCH[1]}"
    return
  fi

  if [[ "$remote_url" =~ ^https?://[^/]+/(.+)$ ]]; then
    echo "${BASH_REMATCH[1]}"
    return
  fi

  echo ""
}

# Detect forge type from host
detect_forge() {
  local host=$1
  local token=$2

  # Known hosts
  case "$host" in
    github.com)
      echo "github"
      return
      ;;
    gitlab.com)
      echo "gitlab"
      return
      ;;
    codeberg.org)
      echo "forgejo"
      return
      ;;
    gitea.com)
      echo "gitea"
      return
      ;;
  esac

  # Unknown host - probe APIs
  local auth_header=""
  if [[ -n "$token" ]]; then
    auth_header="Authorization: Bearer $token"
  fi

  # Try GitLab API first (most common for enterprise self-hosted)
  local gitlab_response
  gitlab_response=$(curl -s -o /dev/null -w "%{http_code}" \
    ${auth_header:+-H "$auth_header"} \
    "https://${host}/api/v4/version" 2>/dev/null)
  if [[ "$gitlab_response" == "200" ]]; then
    echo "gitlab"
    return
  fi

  # Try Gitea/Forgejo API
  local gitea_response
  gitea_response=$(curl -s -o /dev/null -w "%{http_code}" \
    ${auth_header:+-H "$auth_header"} \
    "https://${host}/api/v1/version" 2>/dev/null)
  if [[ "$gitea_response" == "200" ]]; then
    # Check if it's Forgejo (has "forgejo" in version response)
    local version_info
    version_info=$(curl -s ${auth_header:+-H "$auth_header"} \
      "https://${host}/api/v1/version" 2>/dev/null)
    if echo "$version_info" | grep -qi "forgejo"; then
      echo "forgejo"
    else
      echo "gitea"
    fi
    return
  fi

  # Try GitHub Enterprise
  local github_response
  github_response=$(curl -s -o /dev/null -w "%{http_code}" \
    ${auth_header:+-H "$auth_header"} \
    "https://${host}/api/v3/meta" 2>/dev/null)
  if [[ "$github_response" == "200" ]]; then
    echo "github"
    return
  fi

  echo "unknown"
}

# Create release on GitHub
create_github_release() {
  local host=$1
  local repo=$2
  local tag=$3
  local changelog=$4
  local token=$5

  local api_url
  if [[ "$host" == "github.com" ]]; then
    api_url="https://api.github.com"
  else
    api_url="https://${host}/api/v3"
  fi

  echo "Creating GitHub release for tag $tag..."

  local response
  response=$(curl -s -X POST \
    -H "Authorization: Bearer $token" \
    -H "Accept: application/vnd.github+json" \
    -H "Content-Type: application/json" \
    "${api_url}/repos/${repo}/releases" \
    -d "$(jq -n --arg tag "$tag" --arg body "$changelog" \
      '{tag_name: $tag, name: $tag, body: $body, draft: false, prerelease: false}')")

  local release_id
  release_id=$(echo "$response" | jq -r '.id // empty')

  if [[ -z "$release_id" ]]; then
    echo "Error creating release: $(echo "$response" | jq -r '.message // .')" >&2
    return 1
  fi

  echo "$release_id"
}

# Upload asset to GitHub release
upload_github_asset() {
  local host=$1
  local repo=$2
  local release_id=$3
  local file_path=$4
  local token=$5

  local file_name
  file_name=$(basename "$file_path")

  local upload_url
  if [[ "$host" == "github.com" ]]; then
    upload_url="https://uploads.github.com/repos/${repo}/releases/${release_id}/assets?name=${file_name}"
  else
    upload_url="https://${host}/api/uploads/repos/${repo}/releases/${release_id}/assets?name=${file_name}"
  fi

  echo "Uploading $file_name to GitHub release..."

  local response
  response=$(curl -s -X POST \
    -H "Authorization: Bearer $token" \
    -H "Content-Type: application/gzip" \
    --data-binary "@${file_path}" \
    "$upload_url")

  local asset_id
  asset_id=$(echo "$response" | jq -r '.id // empty')

  if [[ -z "$asset_id" ]]; then
    echo "Error uploading asset: $(echo "$response" | jq -r '.message // .')" >&2
    return 1
  fi

  echo "Asset uploaded successfully (ID: $asset_id)"
}

# Create release on GitLab
create_gitlab_release() {
  local host=$1
  local repo=$2
  local tag=$3
  local changelog=$4
  local token=$5

  local api_url="https://${host}/api/v4"
  local encoded_repo
  encoded_repo=$(echo "$repo" | sed 's/\//%2F/g')

  echo "Creating GitLab release for tag $tag..."

  local response
  response=$(curl -s -X POST \
    -H "PRIVATE-TOKEN: $token" \
    -H "Content-Type: application/json" \
    "${api_url}/projects/${encoded_repo}/releases" \
    -d "$(jq -n --arg tag "$tag" --arg desc "$changelog" \
      '{tag_name: $tag, name: $tag, description: $desc}')")

  local release_name
  release_name=$(echo "$response" | jq -r '.name // empty')

  if [[ -z "$release_name" ]]; then
    echo "Error creating release: $(echo "$response" | jq -r '.message // .')" >&2
    return 1
  fi

  echo "$tag"
}

# Upload asset to GitLab release (via Package Registry as Generic Package)
upload_gitlab_asset() {
  local host=$1
  local repo=$2
  local tag=$3
  local file_path=$4
  local token=$5

  local file_name
  file_name=$(basename "$file_path")
  local api_url="https://${host}/api/v4"
  local encoded_repo
  encoded_repo=$(echo "$repo" | sed 's/\//%2F/g')

  echo "Uploading $file_name to GitLab..."

  # Upload to Generic Package Registry
  local package_name="plasma-release"
  local response
  response=$(curl -s -X PUT \
    -H "PRIVATE-TOKEN: $token" \
    --upload-file "$file_path" \
    "${api_url}/projects/${encoded_repo}/packages/generic/${package_name}/${tag}/${file_name}")

  local message
  message=$(echo "$response" | jq -r '.message // empty')

  if [[ -n "$message" && "$message" != "201 Created" ]]; then
    echo "Error uploading asset: $message" >&2
    return 1
  fi

  # Link the asset to the release
  local download_url="${api_url}/projects/${encoded_repo}/packages/generic/${package_name}/${tag}/${file_name}"

  response=$(curl -s -X POST \
    -H "PRIVATE-TOKEN: $token" \
    -H "Content-Type: application/json" \
    "${api_url}/projects/${encoded_repo}/releases/${tag}/assets/links" \
    -d "$(jq -n --arg name "$file_name" --arg url "$download_url" \
      '{name: $name, url: $url, link_type: "package"}')")

  echo "Asset uploaded and linked to release successfully"
}

# Create release on Gitea/Forgejo
create_gitea_release() {
  local host=$1
  local repo=$2
  local tag=$3
  local changelog=$4
  local token=$5

  local api_url="https://${host}/api/v1"

  echo "Creating Gitea/Forgejo release for tag $tag..."

  local response
  response=$(curl -s -X POST \
    -H "Authorization: token $token" \
    -H "Content-Type: application/json" \
    "${api_url}/repos/${repo}/releases" \
    -d "$(jq -n --arg tag "$tag" --arg body "$changelog" \
      '{tag_name: $tag, name: $tag, body: $body, draft: false, prerelease: false}')")

  local release_id
  release_id=$(echo "$response" | jq -r '.id // empty')

  if [[ -z "$release_id" ]]; then
    echo "Error creating release: $(echo "$response" | jq -r '.message // .')" >&2
    return 1
  fi

  echo "$release_id"
}

# Upload asset to Gitea/Forgejo release
upload_gitea_asset() {
  local host=$1
  local repo=$2
  local release_id=$3
  local file_path=$4
  local token=$5

  local file_name
  file_name=$(basename "$file_path")
  local api_url="https://${host}/api/v1"

  echo "Uploading $file_name to Gitea/Forgejo release..."

  local response
  response=$(curl -s -X POST \
    -H "Authorization: token $token" \
    -F "attachment=@${file_path}" \
    "${api_url}/repos/${repo}/releases/${release_id}/assets?name=${file_name}")

  local asset_id
  asset_id=$(echo "$response" | jq -r '.id // empty')

  if [[ -z "$asset_id" ]]; then
    echo "Error uploading asset: $(echo "$response" | jq -r '.message // .')" >&2
    return 1
  fi

  echo "Asset uploaded successfully (ID: $asset_id)"
}

# Find Platform Image (.pi) file
find_image() {
  if [[ ! -d "$IMAGE_DIR" ]]; then
    echo ""
    return
  fi

  # Find the latest .pi file
  local image
  image=$(ls -t "${IMAGE_DIR}"/*.pi 2>/dev/null | head -1)
  echo "$image"
}

# Main script
CUSTOM_TAG=${1}
PREVIEW=${2:-false}
USERNAME=${3}
PASSWORD=${4}
SKIP_UPLOAD=${5:-false}
TOKEN=${6}

# Create temporary askpass script
ASKPASS_SCRIPT=$(mktemp)
cat > "$ASKPASS_SCRIPT" << EOF
#!/bin/bash
case "\$1" in
    *Username*) echo "$USERNAME" ;;
    *Password*) echo "$PASSWORD" ;;
esac
EOF

chmod +x "$ASKPASS_SCRIPT"

# Export the askpass script
export GIT_ASKPASS="$ASKPASS_SCRIPT"

current_branch=$(git rev-parse --abbrev-ref HEAD)
# Check if the current branch is master or main
if [ "$current_branch" != "master" ] && [ "$current_branch" != "main" ]
then
  echo -e "\nError: Current branch is neither 'master' nor 'main', please switch current branch.\n"
  exit 1
fi

ensure_latest_tags
latest_tag=$(semver_get_latest)
if [[ -z "${latest_tag}" ]]; then
  echo "No valid SemVer tags found. Creating initial tag..."

  latest_tag="$INITIAL_TAG"
  echo "Using initial tag: $latest_tag"
else
  echo -e "\nLast tag is: $latest_tag\n"
fi

# Generate changelog with git-cliff
if [[ "$latest_tag" == $INITIAL_TAG ]]; then
  # For initial tag, get all commits
  changelog="$(git cliff --config /action/config.toml)"
else
  changelog="$(git cliff --config /action/config.toml "${latest_tag}"..HEAD)"
fi
changelog=$(echo "${changelog}" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')

if [[ -z "${changelog}" && "$latest_tag" != $INITIAL_TAG ]]; then
  echo "Changelog is empty. Please ensure you have new commits outside latest tag"
  exit 0
fi

echo -e "$changelog\n"

if [ "$PREVIEW" = true ]; then
  exit 0
fi

# Use custom tag or provide choice for user to update.
if [[ -n "${CUSTOM_TAG}" ]]; then
  NEW_TAG=${CUSTOM_TAG}
else
  # Handle initial tag case
  if [[ "$latest_tag" == $INITIAL_TAG ]]; then
    # Creating initial release tag
    NEW_TAG="0.1.0"
  else
    if [[ ${latest_tag:0:1} == "v" ]]; then
      starts_with_v=true
    else
      starts_with_v=false
    fi

    # strip V from tag
    major=$(echo "$latest_tag" | cut -d'.' -f1)
    major=${major#v}

    minor=$(echo "$latest_tag" | cut -d'.' -f2)
    patch=$(echo "$latest_tag" | cut -d'.' -f3)

    patch_tag="${major}.${minor}.$((patch + 1))"
    minor_tag="${major}.$((minor + 1)).0"
    major_tag="$((major + 1)).0.0"

    # return V to tag if any
    if $starts_with_v; then
      patch_tag="v${patch_tag}"
      minor_tag="v${minor_tag}"
      major_tag="v${major_tag}"
    fi

    PS3=$'\nPlease enter your choice: '
    options=("Fix: Safe to upgrade, bugfixes ($patch_tag)" "Feature: Safe to update, new features ($minor_tag)" "Breaking: Not safe to update ($major_tag)")
    on_interrupt() {
      echo -e "\nInterrupted by user, quiting..."
      exit 0
    }

    trap on_interrupt INT

    select opt in "${options[@]}"
    do
      case $opt in
           "${options[0]}")
              echo "Incrementing patch level"
              NEW_TAG=$patch_tag
              break
              ;;
          "${options[1]}")
              echo "Incrementing minor version and reset patch level"
              NEW_TAG=$minor_tag
              break
              ;;
          "${options[2]}")
              echo "Incrementing major version and reset minor and patch level"
              NEW_TAG=$major_tag
              break
              ;;
          *)
              echo "Invalid option $REPLY"
              exit 1
              ;;
            esac
      done
  fi
fi

echo "Creating tag: ${NEW_TAG}"
# Creation of new tag including changelog as description
git tag -f -a $NEW_TAG -m "$changelog"
echo "Press 'Enter' to push new tag to repo"
read -r
git push origin tag $NEW_TAG

# Clean up askpass
rm -f "$ASKPASS_SCRIPT"
unset GIT_ASKPASS

# Handle forge release and artifact upload
if [ "$SKIP_UPLOAD" = true ]; then
  echo -e "\n--skip-upload specified, skipping forge release creation"
  exit 0
fi

# Check for token
if [[ -z "$TOKEN" ]]; then
  echo -e "\nNo API token provided. Skipping forge release creation."
  echo "To create a forge release with artifact upload, provide --token or configure 'release_forge_token' in keyring."
  exit 0
fi

# Detect forge and create release
REMOTE_HOST=$(get_remote_host)
REMOTE_REPO=$(get_remote_repo)

if [[ -z "$REMOTE_HOST" ]] || [[ -z "$REMOTE_REPO" ]]; then
  echo "Error: Could not determine remote host or repository" >&2
  exit 1
fi

echo -e "\nDetecting forge type for $REMOTE_HOST..."
FORGE_TYPE=$(detect_forge "$REMOTE_HOST" "$TOKEN")
echo "Detected forge: $FORGE_TYPE"

if [[ "$FORGE_TYPE" == "unknown" ]]; then
  echo "Error: Could not detect forge type for $REMOTE_HOST" >&2
  echo "Please ensure the host is accessible and supports GitHub/GitLab/Gitea/Forgejo API" >&2
  exit 1
fi

# Create release on forge
RELEASE_ID=""
case "$FORGE_TYPE" in
  github)
    RELEASE_ID=$(create_github_release "$REMOTE_HOST" "$REMOTE_REPO" "$NEW_TAG" "$changelog" "$TOKEN")
    ;;
  gitlab)
    RELEASE_ID=$(create_gitlab_release "$REMOTE_HOST" "$REMOTE_REPO" "$NEW_TAG" "$changelog" "$TOKEN")
    ;;
  gitea|forgejo)
    RELEASE_ID=$(create_gitea_release "$REMOTE_HOST" "$REMOTE_REPO" "$NEW_TAG" "$changelog" "$TOKEN")
    ;;
esac

if [[ -z "$RELEASE_ID" ]]; then
  echo "Failed to create release on $FORGE_TYPE" >&2
  exit 1
fi

echo "Release created successfully (ID/Tag: $RELEASE_ID)"

# Find and upload Platform Image
IMAGE=$(find_image)
if [[ -z "$IMAGE" ]]; then
  echo -e "\nNo Platform Image (.pi) found in $IMAGE_DIR"
  echo "Run 'plasmactl platform:image' first to create an image, or use --skip-upload"
  exit 0
fi

echo -e "\nFound Platform Image: $IMAGE"

case "$FORGE_TYPE" in
  github)
    upload_github_asset "$REMOTE_HOST" "$REMOTE_REPO" "$RELEASE_ID" "$IMAGE" "$TOKEN"
    ;;
  gitlab)
    upload_gitlab_asset "$REMOTE_HOST" "$REMOTE_REPO" "$NEW_TAG" "$IMAGE" "$TOKEN"
    ;;
  gitea|forgejo)
    upload_gitea_asset "$REMOTE_HOST" "$REMOTE_REPO" "$RELEASE_ID" "$IMAGE" "$TOKEN"
    ;;
esac

echo -e "\nRelease $NEW_TAG created successfully with Platform Image!"
