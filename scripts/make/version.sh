#!/bin/sh

# AdGuard DNS CLI Version Generation Script
#
# This script generates versions based on the current git tree state.  The valid
# output formats are:
#
#  *  For release versions, "v0.123.4".  This version should be the one in the
#     current tag, and the script merely checks, that the current commit is
#     properly tagged.
#
#  *  For prerelease alpha versions (aka snapshots), "v0.123.4-a.6+a1b2c3d4".
#

verbose="${VERBOSE:-0}"
readonly verbose

if [ "$verbose" -gt '0' ]; then
	set -x
fi

set -e -f -u

channel="${CHANNEL:?please set CHANNEL}"
readonly channel

case "$channel" in
'development')
	# commit_number is the number of current commit within the branch.
	commit_number="$(git rev-list --count master..HEAD --)"
	readonly commit_number

	# The development builds are described with a combination of unset semantic
	# version, the commit's number within the branch, and the commit hash, e.g.:
	#
	#   v0.0.0-dev.5-a1b2c3d4
	#
	version="v0.0.0-dev.${commit_number}+$(git rev-parse --short HEAD)"
	;;
'release')
	# current_desc is the description of the current git commit.  If the
	# current commit is tagged, git describe will show the tag.
	current_desc="$(git describe)"
	readonly current_desc

	# last_tag is the most recent git tag.
	last_tag="$(git describe --abbrev=0)"
	readonly last_tag

	# Require an actual tag for the beta and final releases.
	if [ "$current_desc" != "$last_tag" ]; then
		echo 'need a tag' 1>&2

		exit 1
	fi

	version="$last_tag"
	;;
'candidate')
	# This pseudo-channel is used to set a proper versions into release
	# candidate builds.

	# current_branch is the name of the branch currently checked out.
	current_branch="$(git rev-parse --abbrev-ref HEAD)"
	readonly current_branch

	# The branch should be named like:
	#
	#   rc-v12.34.56
	#
	if ! echo "$current_branch" | grep -E -e '^rc-v[0-9]+\.[0-9]+\.[0-9]+$' -q; then
		echo "invalid release candidate branch name '$current_branch'" 1>&2

		exit 1
	fi

	version="${current_branch#rc-}-rc.$(git rev-list --count "master"..HEAD)"
	;;
*)
	echo "invalid channel '$channel', supported values are \
		'development', 'edge', 'release' and 'candidate'" 1>&2
	exit 1
	;;
esac

# Finally, make sure that we don't output invalid versions.
if ! echo "$version" | grep -E -e '^v[0-9]+\.[0-9]+\.[0-9]+(-(a|b|dev|rc)\.[0-9]+)?(\+[[:xdigit:]]+)?$' -q; then
	echo "generated an invalid version '$version'" 1>&2

	exit 1
fi

echo "$version"
