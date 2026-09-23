# RVZ → ISO

A lightweight GitHub Pages interface for converting **your own** RVZ game dumps.

The current page is intentionally a UI shell: a real RVZ decoder must be bundled as WebAssembly or another browser-compatible implementation before conversion is enabled. It does not upload game files to a server.

## GitHub Pages

Enable Pages for the `main` branch. GitHub Pages can publish static files directly from a repository.

## Goal

Keep the eventual conversion local in the browser so large game images do not need to travel over a slow Wi-Fi connection.
