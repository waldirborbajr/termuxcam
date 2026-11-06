find . -type f -name '*.go' | sort | while IFS= read -r f; do
  printf -- '----------\n%s\n' "$f"
  cat "$f"
  printf '\n'
done > go_dump.txt