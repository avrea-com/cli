#!/usr/bin/env bash
# Check a generated formula in a temporary tap, without installing it.
set -euo pipefail

if [[ $# != 1 || ! -f $1 ]]; then
  echo "Usage: $0 PATH/TO/avr.rb" >&2
  exit 2
fi

export HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_ANALYTICS=1
validation_tap="avr-validation/formula-$$"
brew tap-new --no-git "$validation_tap" >/dev/null
validation_directory=$(brew --repository "$validation_tap")
cleanup() {
  # Removing our copy first avoids loading untrusted formula code during untap.
  rm -f "$validation_directory/Formula/avr.rb"
  brew untap "$validation_tap" >/dev/null
}
trap cleanup EXIT
cp "$1" "$validation_directory/Formula/avr.rb"
brew style "$validation_tap/avr"
