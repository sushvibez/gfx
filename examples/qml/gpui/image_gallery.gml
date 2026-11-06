// image_gallery — a grid of decoded JPEG thumbnails (GPUI image_gallery)
Rectangle {
    color: "#e9e9e9"; padding: 8; spacing: 6
    Text { text: "Manually managed image cache:"; color: "#000000"; font.pixelSize: 13 }
    Row { spacing: 8; justify: "center"
        Image { source: "../../../build/images/img1.jpg"; width: 104; height: 64 }
        Image { source: "../../../build/images/img2.jpg"; width: 104; height: 64 }
        Image { source: "../../../build/images/img3.jpg"; width: 104; height: 64 }
        Image { source: "../../../build/images/img4.jpg"; width: 104; height: 64 }
    }
    Row { spacing: 8; justify: "center"
        Image { source: "../../../build/images/img5.jpg"; width: 104; height: 64 }
        Image { source: "../../../build/images/img6.jpg"; width: 104; height: 64 }
        Image { source: "../../../build/images/img1.jpg"; width: 104; height: 64 }
        Image { source: "../../../build/images/img2.jpg"; width: 104; height: 64 }
    }
}
