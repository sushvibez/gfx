# qmlgo: Go ports of QML-Examples-Collection pages

The programs here are hand-written Go ports of pages from the
[QML-Examples-Collection](https://github.com/JesusRamosMembrive/QML-Examples-Collection)
corpus (the third render in `scripts/qml-trio.sh`, next to real Qt and the
`.gml` engine). They follow that corpus closely, and the corpus is licensed
under the **GNU General Public License v3.0**, so this directory is too:
see [LICENSE](LICENSE).

This is the one exception to the repository's Apache-2.0 licence (with the
parity screenshots in `script/gml/check/qml-examples`). Nothing in the
framework imports these programs: they live in the separate `examples`
module and are used only to compare renders.
