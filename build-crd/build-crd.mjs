import path from "node:path";

function resolve(obj, dict) {
  if (typeof obj === "object") {
    if (obj["$ref"]) {
      const paths = path.dirname(obj["$ref"].slice(2));
      const key = path.basename(obj["$ref"].slice(2));
      // console.log("REF:", paths, key)
      delete obj["$ref"];
      let d = dict;
      for (const path of paths.split("/")) {
        d = d[path];
      }
      Object.assign(obj, d[key]);
    }
    for (const attr in obj) {
      delete obj[attr]["description"];
      delete obj[attr]["title"];

      if (attr.startsWith('x-') || "readOnly" === attr) {
        delete obj[attr];
      }
      // delete obj[attr]['type']
      resolve(obj[attr], dict);
    }
  }
}

function Frame(names) {
  return {
    apiVersion: "apiextensions.k8s.io/v1",
    kind: "CustomResourceDefinition",
    metadata: {
      name: `${names.plural}.cloudflare.adviser.com`,
    },
    spec: {
      group: "cloudflare.adviser.com",
      versions: [
        {
          name: "v1beta1",
          served: true,
          storage: true,
          schema: {
            openAPIV3Schema: {
              description: `${names.kind} is the Schema for the ${names.kind} API`,
              type: "object",
              properties: {
                apiVersion: {
                  description:
                    "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources",
                  type: "string",
                },
                kind: {
                  description:
                    "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds",
                  type: "string",
                },
                metadata: {
                  type: "object",
                },
                spec: null,
                status: {
                  description: `${names.kind}Status defines the observed state of ${names.kind}`,
                  properties: {
                    conditions: {
                      items: {
                        properties: {
                          lastTransitionTime: {
                            description:
                              "The last time this Condition status changed.",
                            format: "date-time",
                            type: "string",
                          },
                          message: {
                            description:
                              "Full text reason for current status of the condition.",
                            type: "string",
                          },
                          reason: {
                            description:
                              "One word, camel-case reason for current status of the condition.",
                            type: "string",
                          },
                          status: {
                            description: "True, False, or Unknown",
                            type: "string",
                          },
                          type: {
                            description:
                              "Type indicates the scope of the custom resource status addressed by the condition.",
                            type: "string",
                          },
                        },
                        required: ["status", "type"],
                        type: "object",
                      },
                      type: "array",
                    },
                    observedGeneration: {
                      description: `observedGeneration is the most recent successful generation observed for this ${names.kind}. It corresponds to the ${names.kind}'s generation, which is updated on mutation by the API Server.`,
                      format: "int64",
                      type: "integer",
                    },
                  },
                  type: "object",
                },
              },
            },
          },
        },
      ],
      names,
      scope: "Namespaced",
    },
  };
}

function cfd_tunnel(dictCFApi) {
  const dictCrd = Frame({
    categories: ["all", "cloudflare"],
    kind: "CFDTunnel",
    listKind: "CFDTunnelList",
    plural: "cfd-tunnels",
    singular: "cfd-tunnel",
  });
  const out = {
    type: "object",
    ...dictCFApi.paths["/accounts/{account_id}/cfd_tunnel"].post
      .requestBody.content["application/json"].schema,
  };
  resolve(out, dictCFApi);

  dictCrd.spec.versions[0].schema.openAPIV3Schema.properties.spec = out;
  return dictCrd;
}

function cfd_tunnel_config(dictCFApi) {
  const dictCrd = Frame({
    categories: ["all", "cloudflare"],
    kind: "CFDTunnelConfig",
    listKind: "CFDTunnelConfigList",
    plural: "cfd-tunnel-configs",
    singular: "cfd-tunnel-config",
  });
  const out = {
    type: "object",
    ...dictCFApi.paths[
      "/accounts/{account_id}/cfd_tunnel/{tunnel_id}/configurations"
    ].put.requestBody.content["application/json"].schema,
  };
  resolve(out, dictCFApi);

  dictCrd.spec.versions[0].schema.openAPIV3Schema.properties.spec = out;

  return dictCrd;
}

function accessGroup(dictCFApi) {
  const dictCrd = Frame({
    categories: ["all", "cloudflare"],
    kind: "AccessGroup",
    listKind: "AccessGroupList",
    plural: "access-groups",
    singular: "acess-group",
  });
  const out = {
    type: "object",
    ...dictCFApi.paths["/accounts/{account_id}/access/groups"].post.requestBody
      .content["application/json"].schema,
  };
  resolve(out, dictCFApi);

  const includes = {
    type: "object",
    properties: {},
    oneOf: [],
  };
  for (const i of out.properties.include.items.oneOf) {
    const key = Object.keys(i.properties)[0];
    includes.properties[key] = i.properties[key];
    includes.oneOf.push({
      // properties:{},
      required: [key],
    });
  }
  // const api = await OpenAPIParser.validate(out);
  out.properties.include = { type: "array", items: includes };
  out.properties.exclude = { type: "array", items: includes };
  out.properties.require = { type: "array", items: includes };
  dictCrd.spec.versions[0].schema.openAPIV3Schema.properties.spec = out;
  return dictCrd;
}

async function main() {
  const res = await fetch(
    "https://raw.githubusercontent.com/cloudflare/api-schemas/main/openapi.json"
  );
  const dictCFApi = await res.json();
  console.log(
    JSON.stringify(
      {
        apiVersion: "v1",
        items: [
          accessGroup(dictCFApi),
          cfd_tunnel(dictCFApi),
          cfd_tunnel_config(dictCFApi),
        ],
        kind: "List",
        metadata: {
          resourceVersion: "",
        },
      },
      null,
      2
    )
  );
}

main().catch(console.error);
