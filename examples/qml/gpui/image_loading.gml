// image_loading — red-bordered slots: loaded (Zed logo), loading, fallback (GPUI image_loading)
Rectangle {
    color: "#ffffff"; justify: "center"; items: "center"
    Row { spacing: 0; justify: "center"
        Rectangle { width: 48; height: 48; color: "#000000"; border.width: 2; border.color: "#ff0000"; justify: "center"; items: "center"; Image { source: "../../images/assets/logo.png"; width: 44; height: 44 } }
        Rectangle { width: 48; height: 48; color: "#dddddd"; border.width: 2; border.color: "#ff0000" }
        Rectangle { width: 48; height: 48; color: "#dddddd"; border.width: 2; border.color: "#ff0000" }
        Rectangle { width: 48; height: 48; color: "#ffffff"; border.width: 2; border.color: "#ff0000"; justify: "center"; items: "center"; Text { text: "?"; color: "#888888"; font.pixelSize: 14 } }
    }
}
