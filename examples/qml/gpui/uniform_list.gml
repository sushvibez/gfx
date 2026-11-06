// uniform_list — 50 virtualized rows (GPUI uniform_list)
Rectangle {
    color: "#ffffff"
    ListView {
        count: 50; rowHeight: 26; height: 300
        Text { text: "Item " + (index + 1); color: "#000000"; font.pixelSize: 14 }
    }
}
