const APIUSER = process.env.APIUSER && process.env.APIUSER.length ? process.env.APIUSER : undefined;

async function latestRelease(repo, url) {
  const ghUrl = `https://api.github.com${url}`
//   console.error(`URL: ${ghUrl}`)
  const res = await fetch(ghUrl, {
    headers: {
	  ...(
	    typeof(APIUSER) === "string" ? {
	    	"authorization": `Basic ${Buffer.from(APIUSER).toString('base64')}`,
	    } : {}
	  ),
	  // "user-agent": 'Mozilla/5.0 (Macintosh; Intel Mac OS X 11_2_1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/88.0.4324.150 Safari/537.36',
	  "accept": "application/vnd.github.v3+json"
    }
  })
  if (300 < ~~res.status && ~~res.status < 400) {
	// console.log(res.headers['location']);
	return latestRelease(repo, res.headers['location'])
  }
  if (!(200 <= ~~res.status && ~~res.status < 300)) {
		console.error('status:', url, res.status);
  }
  const out = await res.json()
  console.log(`${repo.toUpperCase().replace(/[^A-Z0-9]/g, '_')}_VERSION=${out.tag_name}`);
}

Promise.all(process.argv.slice(2).map(repo => latestRelease(repo, `/repos/${repo}/releases/latest`))).catch(console.error)

