// scrollable — nested 2-way scroll layout (GPUI scrollable). The explicit
// width bounds the viewport so the 2000px-wide panel clips and the view
// scrolls both ways (wheel + horizontal tilt).
Rectangle {
    color: "#ffffff"
    ScrollView {
        scrollbarColor: "#00000080"; padding: 16; spacing: 12; width: 528; height: 468
        Text { text: "Example for test 2 way scroll in nested layout"; color: "#000000" }
        Rectangle {
            height: 2000; border.width: 1; border.color: "#0000ff"; color: "#0000ff0d"; padding: 16; spacing: 16
            Rectangle {
                width: 2000; height: 150; color: "#0080801a"; border.width: 1; border.color: "#008000"; padding: 16
                Text { text: "Scroll Horizontal"; color: "#222222" }
            }
            Text { text: "Scroll Vertical"; color: "#222222" }
        }
    }
}
