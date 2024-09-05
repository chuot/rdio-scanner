Cloudflare Tunnel Example



Here's how to set up a Cloudflare Tunnel on **Windows**:

### 1. **Sign Up for Cloudflare**
   - If you don't have an account, sign up for a free one at [Cloudflare](https://www.cloudflare.com/).

### 2. **Add Your Domain to Cloudflare**
   - Log into the Cloudflare dashboard and add your domain.
   - Update your domain's nameservers to those provided by Cloudflare.

### 3. **Setup And configure the tunnel**

[Tunnel Page](https://one.dash.cloudflare.com/e07280f09998741f418999112bab721d/networks/tunnels/)

-Select "Cloudflared"


  ### 4. **Choose your enviroment and follow the instrustions**
  - I have only tested this with windows
  


  ### 4. **Route your traffic**
- Fill out the hostname example (rdioscanner.yourdomain.tld)
- Fill out the service. the type should be http:// and the URL should be the local ip of your rdio scanner instance 
