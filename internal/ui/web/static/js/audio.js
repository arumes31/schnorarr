// schnorarr - Audio Notification System
// Generates futuristic UI sounds using Web Audio API (No files needed)

const AudioSystem = {
    ctx: null,
    enabled: true,

    init() {
        // Created on first user interaction due to browser policies
        if (!this.ctx) {
            this.ctx = new (window.AudioContext || window.webkitAudioContext)();
        }
        if (this.ctx && this.ctx.state === 'suspended') {
            this.ctx.resume();
        }
    },

    playSuccess() {
        if (!this.enabled || !this.ctx) return;
        
        const osc = this.ctx.createOscillator();
        const gain = this.ctx.createGain();
        
        osc.type = 'sine';
        // Simple clean high-pitched blip
        osc.frequency.setValueAtTime(880, this.ctx.currentTime);
        osc.frequency.exponentialRampToValueAtTime(1200, this.ctx.currentTime + 0.05);
        
        gain.gain.setValueAtTime(0.1, this.ctx.currentTime);
        gain.gain.exponentialRampToValueAtTime(0.001, this.ctx.currentTime + 0.2);
        
        osc.connect(gain);
        gain.connect(this.ctx.destination);
        
        osc.start();
        osc.stop(this.ctx.currentTime + 0.2);
    },

    toggle(btn) {
        this.enabled = !this.enabled;
        if (this.enabled) {
            this.init();
            btn.innerText = '🔊 Audio ON';
            this.playSuccess();
        } else {
            btn.innerText = '🔇 Audio OFF';
        }
    }
};
