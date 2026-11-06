// data_table — header + 11 colour-coded virtualized quote columns (GPUI data_table)
Rectangle {
    color: "#ffffff"
    Row { color: "#f4f4f4"
        Text { text: "id"; width: 36; color: "#777777"; font.pixelSize: 11 }
        Text { text: "symbol"; width: 64; color: "#777777"; font.pixelSize: 11 }
        Text { text: "name"; width: 150; color: "#777777"; font.pixelSize: 11 }
        Text { text: "last_done"; width: 72; color: "#777777"; font.pixelSize: 11 }
        Text { text: "prev_close"; width: 72; color: "#777777"; font.pixelSize: 11 }
        Text { text: "change"; width: 60; color: "#777777"; font.pixelSize: 11 }
        Text { text: "timestamp"; width: 82; color: "#777777"; font.pixelSize: 11 }
        Text { text: "open"; width: 60; color: "#777777"; font.pixelSize: 11 }
        Text { text: "low"; width: 60; color: "#777777"; font.pixelSize: 11 }
        Text { text: "high"; width: 60; color: "#777777"; font.pixelSize: 11 }
        Text { text: "ttm"; width: 60; color: "#777777"; font.pixelSize: 11 }
    }
    ListView {
        count: 200; rowHeight: 22; height: 564
        Row {
            Text { text: "" + (index + 1); width: 36; color: "#111111"; font.pixelSize: 11 }
            Text { text: "SYM" + index; width: 64; color: "#111111"; font.pixelSize: 11 }
            Text { text: "Company " + (index + 1) + " Incorporated"; width: 150; color: "#111111"; font.pixelSize: 11 }
            Text { text: "" + (100 + index); width: 72; color: index % 3 == 0 ? "#cc0000" : "#008000"; font.pixelSize: 11 }
            Text { text: "" + (99 + index); width: 72; color: index % 3 == 0 ? "#cc0000" : "#008000"; font.pixelSize: 11 }
            Text { text: (index % 9 - 4) + "%"; width: 60; color: index % 3 == 0 ? "#cc0000" : "#008000"; font.pixelSize: 11 }
            Text { text: "" + (1718000000 + index * 60); width: 82; color: index % 3 == 0 ? "#cc0000" : "#008000"; font.pixelSize: 11 }
            Text { text: "" + (99 + index); width: 60; color: index % 3 == 0 ? "#cc0000" : "#008000"; font.pixelSize: 11 }
            Text { text: "" + (98 + index); width: 60; color: index % 3 == 0 ? "#cc0000" : "#008000"; font.pixelSize: 11 }
            Text { text: "" + (102 + index); width: 60; color: index % 3 == 0 ? "#cc0000" : "#008000"; font.pixelSize: 11 }
            Text { text: "" + (110 + index); width: 60; color: index % 3 == 0 ? "#cc0000" : "#008000"; font.pixelSize: 11 }
        }
    }
}
