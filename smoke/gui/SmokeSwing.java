import java.awt.Color;
import java.awt.Dimension;
import javax.swing.JFrame;
import javax.swing.JPanel;
import javax.swing.SwingUtilities;

class SmokeSwing {
    public static void main(String[] args) {
        SwingUtilities.invokeLater(() -> {
            JFrame window = new JFrame("SmokeSwing");
            JPanel panel = new JPanel();
            panel.setPreferredSize(new Dimension(320, 160));
            panel.setBackground(new Color(0, 204, 68));
            window.setContentPane(panel);
            window.setDefaultCloseOperation(JFrame.EXIT_ON_CLOSE);
            window.pack();
            window.setVisible(true);
        });
    }
}
