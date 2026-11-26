const assert = require('node:assert/strict');
const { chromium } = require(process.env.HOME + '/.local/share/homebox/playwright/node_modules/playwright');

async function main() {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage();
    const result = await page.evaluate(async (mode) => {
      if (mode === 'stun') {
        const peer = new RTCPeerConnection({
          iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
        });
        try {
          peer.createDataChannel('stun');
          const candidate = new Promise(resolve => {
            peer.onicecandidate = event => {
              if (event.candidate?.type === 'srflx') resolve(true);
              if (!event.candidate) resolve(false);
            };
            setTimeout(() => resolve(false), 12000);
          });
          await peer.setLocalDescription(await peer.createOffer());
          return await candidate;
        } finally {
          peer.close();
        }
      }

      const sender = new RTCPeerConnection();
      const receiver = new RTCPeerConnection();
      const canvas = document.createElement('canvas');
      const stream = canvas.captureStream(15);
      const track = stream.getVideoTracks()[0];
      const context = canvas.getContext('2d');
      const frameTimer = setInterval(() => context.fillRect(0, 0, 20, 20), 50);
      try {
        const received = new Promise(resolve => {
          receiver.ondatachannel = event => {
            event.channel.onmessage = message => resolve(message.data);
          };
        });
        const videoFrame = new Promise(resolve => {
          receiver.ontrack = ({ streams }) => {
            const video = document.createElement('video');
            video.muted = true;
            video.srcObject = streams[0];
            video.requestVideoFrameCallback(() => resolve(true));
            document.body.append(video);
            void video.play();
          };
        });
        const data = sender.createDataChannel('smoke');
        data.onopen = () => data.send('smoke');
        sender.addTrack(track, stream);
        async function gather(peer, description) {
          await peer.setLocalDescription(description);
          if (peer.iceGatheringState !== 'complete') {
            await new Promise(resolve => {
              peer.onicegatheringstatechange = () => {
                if (peer.iceGatheringState === 'complete') resolve();
              };
            });
          }
          return peer.localDescription;
        }
        await receiver.setRemoteDescription(await gather(sender, await sender.createOffer()));
        await sender.setRemoteDescription(await gather(receiver, await receiver.createAnswer()));
        return await Promise.all([received, videoFrame]);
      } finally {
        clearInterval(frameTimer);
        track.stop();
        receiver.close();
        sender.close();
      }
    }, process.argv[2]);
    assert.deepEqual(result, process.argv[2] === 'stun' ? true : ['smoke', true]);
  } finally {
    await browser.close();
  }
}

main().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
