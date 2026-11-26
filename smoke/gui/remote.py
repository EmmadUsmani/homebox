import tkinter as tk
from pathlib import Path

root = tk.Tk()
root.title("Homebox GUI Smoke")
root.geometry("400x240+300+250")
root.configure(background="#00cc44")
entry = tk.Entry(root)
entry.place(x=30, y=30, width=240, height=32)


def save():
    Path("remote-result").write_text(entry.get(), encoding="utf-8")


def paste():
    entry.delete(0, tk.END)
    entry.insert(0, root.clipboard_get())


def copy():
    root.clipboard_clear()
    root.clipboard_append(entry.get())


for x, label, action in [(30, "Save", save), (130, "Paste", paste), (230, "Copy", copy)]:
    tk.Button(root, text=label, command=action).place(x=x, y=85, width=80, height=32)

root.mainloop()
