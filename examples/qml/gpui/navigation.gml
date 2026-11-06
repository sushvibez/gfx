// navigation — TabBar driving a StackLayout, and a StackView with push/pop.
// Shows the single-page navigation containers: only the current page is mounted.
import QtQuick 2.15
import QtQuick.Controls 2.15

Column {
    padding: 20; spacing: 16
    color: "#131318"

    Text { text: "Navigation: TabBar + StackLayout"; color: "#f1f1f7"; font.pixelSize: 18 }

    // A TabBar exposes a currentIndex its TabButtons drive; the StackLayout
    // binds to it and shows only the matching page.
    TabBar {
        id: tabs
        TabButton { text: "Overview" }
        TabButton { text: "Details" }
        TabButton { text: "Settings" }
    }

    Rectangle {
        width: 460; height: 140
        color: "#1d1d27"; radius: 12
        border.width: 1; border.color: "#32323f"

        StackLayout {
            currentIndex: tabs.currentIndex
            padding: 18

            Column {
                spacing: 8
                Text { text: "Overview"; color: "#91a7ff"; font.pixelSize: 16 }
                Text { text: "The current tab is " + tabs.currentIndex; color: "#b6b6c8" }
            }
            Column {
                spacing: 8
                Text { text: "Details"; color: "#69db7c"; font.pixelSize: 16 }
                Text { text: "Only this page is built while it is selected."; color: "#b6b6c8" }
            }
            Column {
                spacing: 8
                Text { text: "Settings"; color: "#ffd43b"; font.pixelSize: 16 }
                Text { text: "Switching tabs swaps which child is live."; color: "#b6b6c8" }
            }
        }
    }

    Text { text: "StackView: push / pop a page stack"; color: "#f1f1f7"; font.pixelSize: 18 }

    // A reusable page pushed onto the StackView at runtime.
    Component {
        id: detailPage
        Rectangle {
            color: "#23202c"; radius: 10; padding: 18
            Column {
                spacing: 8
                Text { text: "Pushed page"; color: "#f783ac"; font.pixelSize: 16 }
                Text { text: "Pop to return to the home page below."; color: "#b6b6c8" }
            }
        }
    }

    Rectangle {
        width: 460; height: 120
        color: "#1d1d27"; radius: 12
        border.width: 1; border.color: "#32323f"

        StackView {
            id: stack
            padding: 6
            initialItem: Rectangle {
                color: "#23202c"; radius: 10; padding: 18
                Column {
                    spacing: 8
                    Text { text: "Home page"; color: "#91a7ff"; font.pixelSize: 16 }
                    Text { text: "Press Push to navigate forward."; color: "#b6b6c8" }
                }
            }
        }
    }

    Row {
        spacing: 10
        Button { text: "Push"; onClicked: stack.push(detailPage) }
        Button { text: "Pop"; color: "#3a3a4a"; onClicked: stack.pop() }
    }
}
