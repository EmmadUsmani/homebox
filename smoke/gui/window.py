import sys

if sys.argv[1] == "tk":
    import tkinter as tk

    root = tk.Tk()
    root.title("SmokeTk")
    root.geometry("320x160")
    root.configure(background="#00cc44")
    root.mainloop()
else:
    from PySide6.QtWidgets import QApplication, QWidget

    app = QApplication([])
    window = QWidget()
    window.setWindowTitle("SmokePySide")
    window.setStyleSheet("background: #00cc44")
    window.resize(320, 160)
    window.show()
    sys.exit(app.exec())
