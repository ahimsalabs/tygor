#!/bin/bash
set -e

# Always run from the script's directory (repo root)
cd "$(dirname "$0")"

# Check that an argument is provided
if [[ $# -eq 0 ]]; then
  echo "Error: No version bump type specified"
  echo "Usage: $0 <major|minor|patch>"
  exit 1
fi

VERSION_TYPE=$1

# Validate version type
if [[ ! "${VERSION_TYPE}" =~ ^(major|minor|patch)$ ]]; then
  echo "Error: Invalid version type '${VERSION_TYPE}'"
  echo "Usage: $0 <major|minor|patch>"
  exit 1
fi

# Check that working directory is clean
# shellcheck disable=SC2312
if [[ -n "$(git status --porcelain)" ]]; then
  echo "Error: Working directory is not clean"
  echo "Please commit or stash your changes before releasing"
  git status --short
  exit 1
fi

# Read current version from VERSION file (single source of truth)
CURRENT_VERSION=$(tr -d '[:space:]' < VERSION)
echo "Current version: ${CURRENT_VERSION}"

# Calculate new version
IFS='.' read -r MAJOR MINOR PATCH <<< "${CURRENT_VERSION}"
case "${VERSION_TYPE}" in
  major) NEW_VERSION="$((MAJOR + 1)).0.0" ;;
  minor) NEW_VERSION="${MAJOR}.$((MINOR + 1)).0" ;;
  patch) NEW_VERSION="${MAJOR}.${MINOR}.$((PATCH + 1))" ;;
  *) echo "Error: unexpected version type"; exit 1 ;;
esac

TAG="v${NEW_VERSION}"
echo "New version: ${NEW_VERSION}"

# Update VERSION files
echo "${NEW_VERSION}" > VERSION
cp VERSION cmd/tygor/VERSION
echo "  Updated VERSION files"

# Update all package.json files
echo ""
echo "Updating package versions..."

# packages/client/package.json
jq --arg v "${NEW_VERSION}" '.version = $v' \
  packages/client/package.json > packages/client/package.json.tmp
mv packages/client/package.json.tmp packages/client/package.json
echo "  Updated packages/client/package.json"

# packages/vite-plugin/package.json (version only, keep workspace:* for local dev)
jq --arg v "${NEW_VERSION}" '.version = $v' \
  packages/vite-plugin/package.json > packages/vite-plugin/package.json.tmp
mv packages/vite-plugin/package.json.tmp packages/vite-plugin/package.json
echo "  Updated packages/vite-plugin/package.json"

# Update all package.json files that depend on @tygor packages
echo "Updating dependent package.json files..."
# shellcheck disable=SC2312
while IFS= read -r pkg_file; do
  # Skip the source packages themselves
  [[ "${pkg_file}" == "packages/client/package.json" ]] && continue
  [[ "${pkg_file}" == "packages/vite-plugin/package.json" ]] && continue

  updated=false
  tmp_file="${pkg_file}.tmp"
  cp "${pkg_file}" "${tmp_file}"

  # Update @tygor/client if present with ^ prefix (not file: or workspace:)
  if jq -e '.dependencies["@tygor/client"] // empty | startswith("^")' "${pkg_file}" > /dev/null 2>&1; then
    jq --arg v "^${NEW_VERSION}" '.dependencies["@tygor/client"] = $v' "${tmp_file}" > "${tmp_file}.2"
    mv "${tmp_file}.2" "${tmp_file}"
    updated=true
  fi

  # Update @tygor/vite-plugin in devDependencies if present with ^ prefix
  if jq -e '.devDependencies["@tygor/vite-plugin"] // empty | startswith("^")' "${pkg_file}" > /dev/null 2>&1; then
    jq --arg v "^${NEW_VERSION}" '.devDependencies["@tygor/vite-plugin"] = $v' "${tmp_file}" > "${tmp_file}.2"
    mv "${tmp_file}.2" "${tmp_file}"
    updated=true
  fi

  # Also check dependencies (some examples use it there)
  if jq -e '.dependencies["@tygor/vite-plugin"] // empty | startswith("^")' "${pkg_file}" > /dev/null 2>&1; then
    jq --arg v "^${NEW_VERSION}" '.dependencies["@tygor/vite-plugin"] = $v' "${tmp_file}" > "${tmp_file}.2"
    mv "${tmp_file}.2" "${tmp_file}"
    updated=true
  fi

  if [[ "${updated}" == "true" ]]; then
    mv "${tmp_file}" "${pkg_file}"
    echo "  Updated ${pkg_file}"
  else
    rm "${tmp_file}"
  fi
done < <(find . -name 'package.json' -not -path '*/node_modules/*' | sed 's|^\./||')

# Update example go.mod files to use the new version
echo ""
echo "Updating example go.mod files..."
# shellcheck disable=SC2312
while IFS= read -r modfile; do
  if grep -q "tygor.dev v" "${modfile}"; then
    sed -i "s|tygor.dev v[0-9.]*|tygor.dev v${NEW_VERSION}|g" "${modfile}"
    echo "  Updated ${modfile}"
  fi
done < <(find examples -name 'go.mod')

# Dry-run builds to catch errors before committing
echo ""
echo "Running dry-run builds..."
(cd packages/client && bun publish --dry-run)
(cd packages/vite-plugin && bun install --ignore-scripts && bun publish --dry-run)
echo "Dry-run builds passed."

# Confirmation
echo ""
read -p "Ready to release ${TAG}? (y/N) " -n 1 -r
echo
if [[ ! ${REPLY} =~ ^[Yy]$ ]]; then
  echo "Aborted. Restoring changes..."
  git checkout VERSION packages/client/package.json packages/vite-plugin/package.json examples/
  exit 1
fi

# Commit and tag
git commit -a -m "Release ${TAG}"
git tag "${TAG}"

# Publish both packages (use bun to resolve workspace:* dependencies)
echo ""
echo "Publishing @tygor/client..."
pushd packages/client
bun publish
popd

echo ""
echo "Publishing @tygor/vite-plugin..."
pushd packages/vite-plugin
bun install --ignore-scripts
bun publish
popd

echo ""
echo "Successfully released ${TAG}. Run: git push && git push --tags"
