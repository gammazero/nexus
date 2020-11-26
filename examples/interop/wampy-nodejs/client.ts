import { Wampy } from "wampy";

export class WampClient {
    connectP: Promise<any>;
    ws: any;

    constructor(private options: { debug?: boolean, port?:number, realm?:string } = { debug: false, port: 8080 }) {
    }

    connect(): Promise<any> {
        if (!this.connectP)
            this.connectP = new Promise<any>((resolve, reject) => {
                this.log("connecting...");
                let w3cws = require("websocket").w3cwebsocket;
                this.ws = new Wampy("ws://127.0.0.1:"+this.options.port+"/", {
                    ws: w3cws,
                    realm: this.options.realm||"nexus.example",
                    onConnect: () => {
                        this.log("connection - connect success");
                        resolve(this.ws);
                    },
                    onError: err => {
                        this.log("connection - error!");
                        reject(err);
                    },
                    onClose: () => {
                        this.log("connection - closed");
                    },
                    debug: false,
                    autoReconnect: true
                });
            });
        return this.connectP;
    }

    close() {
        this.log("closing");
        this.ws.disconnect();
        this.connectP = null;
        this.ws = null;
    }

    log(...args) {
        console.log("[broker]", ...args);
    }

    subscribe(topic, callback) {
        this.log("Subscribing to ", topic);
        this.connect().then(ws => {
            ws.subscribe(topic, v => {
                this.log("Message received");
                callback(v);
            });
        });
    }

    rpc(method,args = {}): Promise<any> {
        return this.connect().then( ws => 
            new Promise<any>((resolve, reject) => {
                let r = ws.call( method, args,
                    {
                        onSuccess: (dataArr, dataObj) => {
                            let result = dataArr.argsList;
                            resolve(...result);
                        },
                        onError: (err, detailsObj) => {
                            this.log("call failed with error ", err);
                            reject(err);
                        }
                    }
                );
            })
        );
    }
}
