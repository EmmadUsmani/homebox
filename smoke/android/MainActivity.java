package dev.homebox.smoke;

import android.app.Activity;
import android.os.Bundle;
import android.view.Gravity;
import android.widget.TextView;

public final class MainActivity extends Activity {
    @Override
    public void onCreate(Bundle state) {
        super.onCreate(state);
        TextView message = new TextView(this);
        message.setText("Homebox Android smoke");
        message.setTextSize(26);
        message.setGravity(Gravity.CENTER);
        setContentView(message);
    }
}
