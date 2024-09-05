# Setting Up a Cloudflare Tunnel on Windows

### 1. Sign Up for Cloudflare
- If you don't have an account, sign up for a free one at [Cloudflare](https://www.cloudflare.com).

### 2. Add Your Domain to Cloudflare
- Log into the Cloudflare dashboard and add your domain.
- Update your domain's nameservers to those provided by Cloudflare.

### 3. Set Up and Configure the Tunnel
- Go to the **Tunnels** page in the Cloudflare dashboard.
- Select **Cloudflared** to create a tunnel.

### 4. Choose Your Environment
- Follow the instructions for your environment (only tested with **Windows**).

### 5. Route Your Traffic
- Fill out the **hostname** (example: `rdioscanner.yourdomain.tld`).
- For the **service type**, select `http://`.
- Input the **local IP** of your rdio scanner instance in the **URL** field.
