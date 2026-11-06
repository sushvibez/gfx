#!/usr/bin/env python3
"""Turn an interaction script into a QtTest TestCase for qmltestrunner.

The same step file drives our engine (examples/qml -script) and this, so a
per-step image pair compares the two engines' response to identical input
rather than two hand-written scripts that drifted apart.
"""
import sys, os

KEYS = {
    "backspace": "Backspace", "delete": "Delete", "left": "Left", "right": "Right",
    "up": "Up", "down": "Down", "home": "Home", "end": "End", "enter": "Return",
    "return": "Return", "tab": "Tab", "escape": "Escape", "esc": "Escape",
    "pageup": "PageUp", "pagedown": "PageDown", "insert": "Insert",
    **{f"f{i}": f"F{i}" for i in range(1, 13)},
}
MODS = {"ctrl": "Qt.ControlModifier", "control": "Qt.ControlModifier",
        "shift": "Qt.ShiftModifier", "alt": "Qt.AltModifier"}


def steps(path):
    out = []
    for raw in open(path):
        line = raw.split("#", 1)[0].strip()
        if line:
            out.append(line)
    return out


def xy(tok):
    x, y = tok.split(",")
    return float(x), float(y)


def key_call(spec):
    parts = spec.split("+")
    mods = " | ".join(MODS[m.lower()] for m in parts[:-1]) or "Qt.NoModifier"
    name = parts[-1]
    if name.lower() in KEYS:
        return f'keyClick(Qt.Key_{KEYS[name.lower()]}, {mods})'
    return f'keyClick("{name}", {mods})'


def emit(line, i, shots):
    v = line.split()
    verb = v[0].lower()
    shot = f'        shoot("{shots}/{i:02d}.png");'
    body = []
    if verb == "click":
        x, y = xy(v[1]); body = [f'        mouseClick(page, {x}, {y}, Qt.LeftButton);']
    elif verb == "press":
        x, y = xy(v[1]); body = [f'        mousePress(page, {x}, {y}, Qt.LeftButton);']
    elif verb == "release":
        x, y = xy(v[1]); body = [f'        mouseRelease(page, {x}, {y}, Qt.LeftButton);']
    elif verb == "move":
        x, y = xy(v[1]); body = [f'        mouseMove(page, {x}, {y});']
    elif verb in ("drag", "fling"):
        x0, y0 = xy(v[1]); x1, y1 = xy(v[2])
        # mouseDrag synthesises the intermediate moves Qt's own gesture
        # recognisers need; a flick differs only in how fast it lets go.
        hold = 0 if verb == "fling" else 30
        body = [f'        mouseDrag(page, {x0}, {y0}, {x1 - x0}, {y1 - y0}, Qt.LeftButton, Qt.NoModifier, {hold});']
    elif verb == "wheel":
        x, y = xy(v[1])
        # Our positive delta scrolls the content down, which is a wheel
        # rotation TOWARD the user — a negative angle delta in Qt.
        body = [f'        mouseWheel(page, {x}, {y}, 0, {-float(v[2])});']
    elif verb == "key":
        body = [f'        {key_call(v[1])}']
    elif verb == "type":
        text = line[len(v[0]):].strip()
        body = [f'        keyClick("{c}");' for c in text]
    elif verb == "wait":
        body = []
    else:
        sys.exit(f"qml_interact_gen: unknown step {line!r}")
    return body + ['        settle();', shot]


def main():
    script, page, shots = sys.argv[1], sys.argv[2], os.path.abspath(sys.argv[3])
    lines = ["import QtQuick", "import QtTest", "", "TestCase {",
             "    id: tc", '    name: "interact"', "    width: 900; height: 700",
             "    when: windowShown", "    visible: true", "",
             '    Rectangle { anchors.fill: parent; color: "#2b2d31" }',
             "    Loader {", "        id: page", "        anchors.fill: parent",
             f'        source: "file://{os.path.abspath(page)}"',
             '        onLoaded: if (item && "fullSize" in item) item.fullSize = true',
             "    }", "",
             "    function shoot(path) { var img = grabImage(tc); img.save(path); }",
             "    // A step's effect is often animated (a tab sliding, a popup",
             "    // opening): let it land before the capture, the way our",
             "    // settle loop does.",
             "    function settle() { wait(700); }", "",
             "    function test_interact() {",
             "        wait(2500);",
             f'        shoot("{shots}/00.png");']
    for i, line in enumerate(steps(script), start=1):
        lines += emit(line, i, shots)
    lines += ["    }", "}"]
    print("\n".join(lines))


main()
