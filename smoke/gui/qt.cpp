#include <QApplication>
#include <QWidget>

int main(int argc, char **argv) {
    QApplication app(argc, argv);
    QWidget window;
    window.setWindowTitle("SmokeQt");
    window.setStyleSheet("background: #00cc44");
    window.resize(320, 160);
    window.show();
    return app.exec();
}
