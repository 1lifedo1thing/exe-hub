# Type in the preview pictures

- Go Sans (regular and bold) comes from `golang.org/x/image/font/gofont`,
  under the Go fonts licence that module carries.
- `DroidSansFallback.ttf` is Google's Droid Sans Fallback, copied from
  Debian's `fonts-droid-fallback` package, Apache License 2.0
  (https://www.apache.org/licenses/LICENSE-2.0). It supplies the CJK
  characters Go Sans lacks.
- `emoji.zip` holds the pictures of Google's Noto Color Emoji 2.047,
  cut unchanged from Ubuntu's `fonts-noto-color-emoji` package by
  `genemoji.py`, SIL Open Font License 1.1
  (https://openfontlicense.org); Google also publishes the same
  pictures under the Apache License 2.0
  (https://github.com/googlefonts/noto-emoji). Which sequence is which
  picture comes from Unicode 16.0's emoji-test.txt, and
  `emoji_presentation.go` from its emoji-data.txt (Unicode License v3,
  https://www.unicode.org/license.txt).
