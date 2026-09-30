#!/usr/bin/env python3
"""The hub's 88x31 badge: a bulletin board, dialled into.

    python3 badge.py                  writes badge.gif beside this file
    python3 badge.py sheet.png        also writes every frame, 4x, to look at

A hub is what a BBS was: a board anyone may call, read and post to. So
the badge is a DOS text screen in the sixteen VGA colours. It opens on
the board's welcome screen (the first frame is the whole badge, for a
reader who stops animations): the hub's own icon, "exe-hub" in ANSI
bands with a dithered shadow, and PRESS ANY KEY, which on a hub is the
truth, since a key is the account. A post comes in along a spoke of the
icon and lights the word. Then the line drops, NO CARRIER, and the modem
dials the hub's port (ATDT 7788), connects at 28800, and the screen
comes down the wire a character cell at a time, the way ANSI art did.
Needs Pillow; nothing else.
"""
import os
import sys

from PIL import Image

W, H = 88, 31

# the sixteen colours of a VGA text screen, and nothing else
PAL = {
    '0': (0x00, 0x00, 0x00), '1': (0x00, 0x00, 0xaa), '2': (0x00, 0xaa, 0x00), '3': (0x00, 0xaa, 0xaa),
    '4': (0xaa, 0x00, 0x00), '5': (0xaa, 0x00, 0xaa), '6': (0xaa, 0x55, 0x00), '7': (0xaa, 0xaa, 0xaa),
    '8': (0x55, 0x55, 0x55), '9': (0x55, 0x55, 0xff), 'a': (0x55, 0xff, 0x55), 'b': (0x55, 0xff, 0xff),
    'c': (0xff, 0x55, 0x55), 'd': (0xff, 0x55, 0xff), 'e': (0xff, 0xff, 0x55), 'f': (0xff, 0xff, 0xff),
}
ORDER = list(PAL)

# "exe-hub" as an ANSI logo: 2px stems, ten rows, coloured in bands
LOGO = {
    'e': ["......", "......", "......",
          ".####.", "##..##", "######", "##....", "##....", "##..##", ".####."],
    'x': ["......", "......", "......",
          "##..##", "##..##", ".####.", "..##..", ".####.", "##..##", "##..##"],
    '-': ["....", "....", "....", "....", "....", "....", "####", "####", "....", "...."],
    'h': ["##....", "##....", "##....",
          "##.##.", "######", "##..##", "##..##", "##..##", "##..##", "##..##"],
    'u': ["......", "......", "......",
          "##..##", "##..##", "##..##", "##..##", "##..##", "##.###", ".##.##"],
    'b': ["##....", "##....", "##....",
          "#####.", "##..##", "##..##", "##..##", "##..##", "##..##", "#####."],
}
BANDS = ['b', 'b', 'f', 'f', 'b', 'b', '3', '3', '9', '1']
LX, LY = 30, 5

# the screen's own type: three by five, four for N and M
FONT = {
    'A': [".#.", "#.#", "###", "#.#", "#.#"], 'C': [".##", "#..", "#..", "#..", ".##"],
    'D': ["##.", "#.#", "#.#", "#.#", "##."], 'E': ["###", "#..", "##.", "#..", "###"],
    'I': ["###", ".#.", ".#.", ".#.", "###"], 'K': ["#.#", "#.#", "##.", "#.#", "#.#"],
    'N': ["#..#", "##.#", "#.##", "#..#", "#..#"], 'O': [".#.", "#.#", "#.#", "#.#", ".#."],
    'P': ["##.", "#.#", "##.", "#..", "#.."], 'R': ["##.", "#.#", "##.", "#.#", "#.#"],
    'S': [".##", "#..", ".#.", "..#", "##."], 'T': ["###", ".#.", ".#.", ".#.", ".#."],
    'Y': ["#.#", "#.#", ".#.", ".#.", ".#."],
    '0': [".#.", "#.#", "#.#", "#.#", ".#."], '2': ["##.", "..#", ".#.", "#..", "###"],
    '7': ["###", "..#", ".#.", ".#.", ".#."], '8': ["###", "#.#", "###", "#.#", "###"],
    ' ': ["..", "..", "..", "..", ".."],
}
# the three shade characters, as a text screen draws them in four by five
SHADE = {
    3: ["###.", "#.##", "###.", "#.##", "###."],  # dark shade
    2: ["#.#.", ".#.#", "#.#.", ".#.#", "#.#."],  # medium shade
    1: ["#...", "..#.", "#...", "..#.", "#..."],  # light shade
}
STATUS_Y = 20

# the hub's icon on a tile of its own: four nodes on the corners, one in
# the middle, a dotted spoke from each corner in
GX, GY, GS = 12, 4, 13
CORNERS = [(1, 1), (GS - 3, 1), (GS - 3, GS - 3), (1, GS - 3)]
# the dots of the spoke from each corner in, outermost first
SPOKES = [[(3, 3), (4, 4)], [(GS - 4, 3), (GS - 5, 4)], [(GS - 4, GS - 4), (GS - 5, GS - 5)], [(3, GS - 4), (4, GS - 5)]]

CELL_W, CELL_H = 4, 6  # a character cell, for the screen coming down the wire


def blank():
    return [['0'] * W for _ in range(H)]


def put(img, x, y, c):
    if 0 <= x < W and 0 <= y < H:
        img[y][x] = c


def text(img, s, x, y, col):
    for ch in s:
        g = FONT[ch]
        for r, row in enumerate(g):
            for i, c in enumerate(row):
                if c == '#':
                    put(img, x + i, y + r, col)
        x += len(g[0]) + 1
    return x


def text_w(s):
    return sum(len(FONT[c][0]) + 1 for c in s) - 1


def cursor(img, x, y, col='7'):
    for r in range(5):
        for i in range(3):
            put(img, x + i, y + r, col)


def border(img):
    for x in range(W):
        img[0][x] = img[H - 1][x] = '9'
        if 1 < x < W - 2:
            img[2][x] = img[H - 3][x] = '9'
    for y in range(H):
        img[y][0] = img[y][W - 1] = '9'
        if 1 < y < H - 2:
            img[y][2] = img[y][W - 3] = '9'


def logo(img, bands):
    word = "exe-hub"
    mask = ['.'.join(LOGO[c][r] for c in word) for r in range(10)]
    for r in range(10):
        for i, c in enumerate(mask[r]):
            sx, sy = LX + i + 1, LY + r + 1
            if c == '#' and (sx + sy) % 2 == 0 and img[sy][sx] == '0':
                img[sy][sx] = '8'  # the shadow, one down and one right, dithered
    for r in range(10):
        for i, c in enumerate(mask[r]):
            if c == '#':
                img[LY + r][LX + i] = bands[r]


def glyph(img, packet=None, flash=False):
    """packet is (spoke, step): a post on its way in, step 0 at the
    corner node, 1 and 2 on the dots; flash lights the middle node."""
    for y in range(GS):
        for x in range(GS):
            img[GY + y][GX + x] = '1'
    for n, dots in enumerate(SPOKES):
        for x, y in dots:
            img[GY + y][GX + x] = '9'
    for n, (x, y) in enumerate(CORNERS):
        hot = packet is not None and packet[0] == n and packet[1] == 0
        for dy in range(2):
            for dx in range(2):
                img[GY + y + dy][GX + x + dx] = 'e' if hot else 'b'
        img[GY + y][GX + x] = 'f'
    if packet is not None and packet[1] in (1, 2):
        x, y = SPOKES[packet[0]][packet[1] - 1]
        img[GY + y][GX + x] = 'e'
    for dy in range(3):
        for dx in range(3):
            img[GY + 5 + dy][GX + 5 + dx] = 'e' if flash else '9'
    img[GY + 5][GX + 5] = 'f'


def status(img, on):
    s1, s2 = "PRESS ", "ANY KEY"
    w = text_w(s1 + s2) + 4  # and the cursor
    x = (W - w) // 2
    # the shades either side, clear of the frame by a pixel and of the
    # words by one more
    for n, lv in enumerate([3, 2, 1]):
        shade(img, x - 13 + n * 4, STATUS_Y, lv)
    for n, lv in enumerate([1, 2, 3]):
        shade(img, x + w + 1 + n * 4, STATUS_Y, lv)
    e = text(img, s1, x, STATUS_Y, '7')
    e = text(img, s2, e, STATUS_Y, 'e')
    if on:
        cursor(img, e, STATUS_Y)


def shade(img, x, y, level):
    for r, row in enumerate(SHADE[level]):
        for i, c in enumerate(row):
            if c == '#':
                put(img, x + i, y + r, '9')


def screen(on=True, packet=None, flash=False, bands=BANDS):
    img = blank()
    border(img)
    glyph(img, packet, flash)
    logo(img, bands)
    status(img, on)
    return img


def terminal(lines, typing=None, on=True):
    """The modem's side of it: grey lines on black, the cursor after the
    last, typing the line given so far."""
    img = blank()
    y = 4
    for s in lines:
        text(img, s, 4, y, '7')
        y += 7
    x = 4
    if typing is not None:
        x = text(img, typing, 4, y, '7') + (1 if typing else 0)
    elif lines:
        y -= 7
        x = text(img, lines[-1], 4, y, '7') + 1
    if on:
        cursor(img, x, y)
    return img


def painted(final, cells):
    """The screen as far as the wire has brought it: the first cells in
    reading order, and the cursor where the next goes."""
    img = blank()
    cols = (W + CELL_W - 1) // CELL_W
    for n in range(cells):
        cx, cy = (n % cols) * CELL_W, (n // cols) * CELL_H
        for y in range(cy, min(cy + CELL_H, H)):
            for x in range(cx, min(cx + CELL_W, W)):
                img[y][x] = final[y][x]
    cx, cy = (cells % cols) * CELL_W, (cells // cols) * CELL_H
    for y in range(cy + CELL_H - 2, min(cy + CELL_H, H)):  # an underline cursor, as DOS had it
        for x in range(cx, min(cx + CELL_W - 1, W)):
            img[y][x] = '7'
    return img


def timeline():
    out = []
    add = lambda f, cs: out.append((f, cs))

    # the welcome screen, whole
    add(screen(True), 70)
    add(screen(False), 40)
    # a post comes in along a spoke, and the word lights up
    for spoke in (1, 3):
        for step in range(3):
            add(screen(step != 1, packet=(spoke, step)), 9)
        add(screen(True, flash=True), 8)
        # the bands roll down the word once, a palette cycle
        for k in range(1, len(BANDS)):
            rolled = BANDS[-k:] + BANDS[:-k]
            add(screen(k % 4 < 2, bands=rolled), 5)
        add(screen(True), 45)
        add(screen(False), 40)
    add(screen(True), 60)

    # the line drops, and the modem calls again
    add(blank(), 25)
    add(terminal(["NO CARRIER"]), 70)
    for n in range(1, len("ATDT 7788") + 1):
        add(terminal(["NO CARRIER"], typing="ATDT 7788"[:n]), [9, 7, 8, 10, 6, 12, 8, 7, 9][n - 1])
    for on in (False, True, False, True, False):
        add(terminal(["NO CARRIER", "ATDT 7788"], typing="", on=on), 30)
    add(terminal(["NO CARRIER", "ATDT 7788", "CONNECT 28800"]), 55)
    add(blank(), 12)

    # the screen, down the wire: eleven cells a frame
    final = screen(True)
    cols, rows = (W + CELL_W - 1) // CELL_W, (H + CELL_H - 1) // CELL_H
    total = cols * rows
    for cells in range(0, total, 11):
        add(painted(final, cells), 4)
    return out


def to_image(f):
    im = Image.new('P', (W, H))
    flat = []
    for c in ORDER:
        flat.extend(PAL[c])
    im.putpalette(flat)
    im.putdata([ORDER.index(c) for row in f for c in row])
    return im


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    frames = timeline()
    ims = [to_image(f) for f, _ in frames]
    out = os.path.join(here, 'badge.gif')
    ims[0].save(out, save_all=True, append_images=ims[1:],
                duration=[cs * 10 for _, cs in frames], loop=0, disposal=1, optimize=False)
    if len(sys.argv) > 1:
        scale, cols = 4, 4
        rows = (len(ims) + cols - 1) // cols
        sheet = Image.new('RGB', (cols * (W * scale + 8), rows * (H * scale + 8)), (0x40, 0x40, 0x40))
        for i, im in enumerate(ims):
            big = im.convert('RGB').resize((W * scale, H * scale), Image.NEAREST)
            sheet.paste(big, ((i % cols) * (W * scale + 8), (i // cols) * (H * scale + 8)))
        sheet.save(sys.argv[1])
    total = sum(cs for _, cs in frames)
    print(f"{len(frames)} frames, {total / 100:.1f} s a loop, {os.path.getsize(out)} bytes")


if __name__ == '__main__':
    main()
