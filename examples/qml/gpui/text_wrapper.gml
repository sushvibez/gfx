// text_wrapper — ellipsis boxes + clamp rows (ellipsis/truncate/nowrap) + full wrap (GPUI text_wrapper)
Rectangle {
    color: "#ffffff"; padding: 8; spacing: 8
    property string t: "The longest word 你好世界这段是中文，こんにちは in any of the major English language dictionaries is pneumonoultramicroscopicsilicovolcanoconiosis, a word that refers to a lung disease contracted from the inhalation of very fine silica particles, specifically from a volcano; medically, it is the same as silicosis."
    Row { spacing: 8
        Rectangle { width: 160; padding: 4; border.width: 1; border.color: "#ff0000"; Text { text: "longer text in flex 1"; color: "#000000"; width: 150; elide: true; font.pixelSize: 14 } }
        Rectangle { width: 90; padding: 4; border.width: 1; border.color: "#ff0000"; Text { text: "short flex"; color: "#000000"; width: 80; elide: true; font.pixelSize: 14 } }
        Rectangle { width: 220; padding: 4; border.width: 1; border.color: "#ff0000"; Text { text: "A short text in normal div"; color: "#000000"; width: 210; elide: true; font.pixelSize: 14 } }
    }
    Rectangle { width: 784; padding: 3; border.width: 1; border.color: "#0000ff"; Text { text: "ELLIPSIS: " + t; color: "#000000"; width: 776; wrapMode: "WordWrap"; maximumLineCount: 1; font.pixelSize: 18 } }
    Rectangle { width: 784; padding: 3; border.width: 1; border.color: "#0000ff"; Text { text: "ELLIPSIS 2 lines: " + t; color: "#000000"; width: 776; wrapMode: "WordWrap"; maximumLineCount: 2; font.pixelSize: 18 } }
    Rectangle { width: 784; padding: 3; border.width: 1; border.color: "#008000"; Text { text: "TRUNCATE: " + t; color: "#000000"; width: 776; wrapMode: "WordWrap"; maximumLineCount: 1; font.pixelSize: 18 } }
    Rectangle { width: 784; padding: 3; border.width: 1; border.color: "#008000"; Text { text: "TRUNCATE 3 lines: " + t; color: "#000000"; width: 776; wrapMode: "WordWrap"; maximumLineCount: 3; font.pixelSize: 18 } }
    Rectangle { width: 784; padding: 3; border.width: 1; border.color: "#000000"; Text { text: "NOWRAP: " + t; color: "#000000"; width: 776; wrapMode: "WordWrap"; maximumLineCount: 1; font.pixelSize: 18 } }
    Text { text: t; color: "#000000"; width: 784; wrapMode: "WordWrap"; font.pixelSize: 18 }
}
