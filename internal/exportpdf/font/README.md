# PDF Font Provenance

The bundled Noto Sans SC Regular font is a static 400-weight instance of
Google Fonts' `ofl/notosanssc/NotoSansSC[wght].ttf`. The upstream file is
17,772,300 bytes, Git blob `fb0637bafbcd804fe32152370a1225990745b4bc`,
SHA-256 `a3041811a78c361b1de50f953c805e0244951c21c5bd412f7232ef0d899af0da`.

The static instance was produced with fontTools 4.68.0:

```sh
fonttools varLib.instancer 'NotoSansSC[wght].ttf' wght=400 --static --update-name-table --no-recalc-timestamp -o NotoSansSC-Regular.ttf
gzip -n -k NotoSansSC-Regular.ttf
```

The resulting TTF is 10,595,876 bytes, SHA-256
`26305be85f5378dc7d53f3dfa6c852a7e46621b226423400c1821aca9fc01c41`.
The gzip asset is 6,318,677 bytes. The primary family name is Noto Sans SC,
not the license's reserved name `Source`. This modified font remains under the
SIL Open Font License 1.1 in `OFL.txt`; the unsigned macOS package also places
the full notice in `Contents/Resources/PDF-Font-OFL.txt`.
