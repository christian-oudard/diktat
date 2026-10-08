# Model menu

`diktat model` with no argument lists the menu, one row per model, ordered
roughly by download size. Each row shows:

- `#` - the number that selects the model, as in `diktat model 3`.
- `Name`
- `Size` - the download, in binary units.
- `Downloaded` - marked when the model is in the cache.
- `Languages` - the language set, as its reach and its size: a language name
  for one or two, `European (n)`, `Worldwide (n)`, or `Worldwide` for a model
  that takes about a hundred.
- `Features` - what the model can do beyond transcribing a finished clip.
  `streaming` means it can transcribe audio while it is still arriving.

A `*` before the number marks the model in use, and a `>` marks one being
loaded.

The menu answers before anything is downloaded, so languages and features are
kept by hand, and a test checks them against the library for every model in
the cache.
