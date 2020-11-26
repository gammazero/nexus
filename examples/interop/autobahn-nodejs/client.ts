import { Connection } from "autobahn";

export class AuthobanClient {

    connectP: Promise<any>;
    connection: Connection;

    constructor(private options: { debug?: boolean, port?:number, realm?:string } = { debug: false, port: 8080 }) {
    }

    connect(): Promise<any> {
        if (!this.connectP) {

            this.connectP = new Promise((resolve,reject)=>{

                let realm = this.options.realm||"nexus.example"
                let url = "ws://127.0.0.1:"+this.options.port+"/"

                let connection = new Connection({url, realm});
                this.connection = connection;

                connection.onopen = session=>{
                    this.log("connection - connect success");
                    resolve(session);
                }

                connection.onclose = (reason, details)=>{
                    this.log("connection - closed: ",reason);
                }

                connection.open();
            });
        }


        return this.connectP;
    }

    close() {
        this.log("closing");
        this.connection.close();
        this.connection = null;
        this.connectP = null;
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
        return this.connect().then( ws => {
            return ws.call( method, args  ).catch(e=>{
                throw(e.error)
            });
        });
    }
}
