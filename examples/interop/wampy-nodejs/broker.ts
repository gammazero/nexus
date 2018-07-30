import { Wampy } from "wampy";

export class Broker {
    connectP: Promise<any>;
    ws: any;

    constructor(private options: { debug?: boolean, port?:number } = { debug: false, port: 8008 }) {
    }

    connect(): Promise<any> {
        if (!this.connectP)
            this.connectP = new Promise<any>((resolve, reject) => {
                this.log("connecting...");
                let w3cws = require("websocket").w3cwebsocket;
                this.ws = new Wampy("ws://127.0.0.1:"+this.options.port+"/", {
                    ws: w3cws,
                    realm: "nexus.example",
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
                            let status = dataArr.argsList[0];
                            let res = dataArr.argsList[1];
                            let body;
                            try {
                                body = JSON.parse(res);
                            } catch (e) {
                                reject(e);
                                return
                            }

                            if (status == "error") {
                                reject(JSON.parse(res));
                            } else {
                                resolve(JSON.parse(res));
                            }
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