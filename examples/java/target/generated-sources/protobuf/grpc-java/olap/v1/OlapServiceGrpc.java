package olap.v1;

import static io.grpc.MethodDescriptor.generateFullMethodName;

/**
 */
@javax.annotation.Generated(
    value = "by gRPC proto compiler (version 1.53.0)",
    comments = "Source: olap.proto")
@io.grpc.stub.annotations.GrpcGenerated
public final class OlapServiceGrpc {

  private OlapServiceGrpc() {}

  public static final String SERVICE_NAME = "olap.v1.OlapService";

  // Static method descriptors that strictly reflect the proto.
  private static volatile io.grpc.MethodDescriptor<olap.v1.Olap.WriteRequest,
      olap.v1.Olap.WriteResponse> getWriteMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "Write",
      requestType = olap.v1.Olap.WriteRequest.class,
      responseType = olap.v1.Olap.WriteResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<olap.v1.Olap.WriteRequest,
      olap.v1.Olap.WriteResponse> getWriteMethod() {
    io.grpc.MethodDescriptor<olap.v1.Olap.WriteRequest, olap.v1.Olap.WriteResponse> getWriteMethod;
    if ((getWriteMethod = OlapServiceGrpc.getWriteMethod) == null) {
      synchronized (OlapServiceGrpc.class) {
        if ((getWriteMethod = OlapServiceGrpc.getWriteMethod) == null) {
          OlapServiceGrpc.getWriteMethod = getWriteMethod =
              io.grpc.MethodDescriptor.<olap.v1.Olap.WriteRequest, olap.v1.Olap.WriteResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(SERVICE_NAME, "Write"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  olap.v1.Olap.WriteRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  olap.v1.Olap.WriteResponse.getDefaultInstance()))
              .setSchemaDescriptor(new OlapServiceMethodDescriptorSupplier("Write"))
              .build();
        }
      }
    }
    return getWriteMethod;
  }

  private static volatile io.grpc.MethodDescriptor<olap.v1.Olap.QueryRequest,
      olap.v1.Olap.QueryResponse> getQueryMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "Query",
      requestType = olap.v1.Olap.QueryRequest.class,
      responseType = olap.v1.Olap.QueryResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<olap.v1.Olap.QueryRequest,
      olap.v1.Olap.QueryResponse> getQueryMethod() {
    io.grpc.MethodDescriptor<olap.v1.Olap.QueryRequest, olap.v1.Olap.QueryResponse> getQueryMethod;
    if ((getQueryMethod = OlapServiceGrpc.getQueryMethod) == null) {
      synchronized (OlapServiceGrpc.class) {
        if ((getQueryMethod = OlapServiceGrpc.getQueryMethod) == null) {
          OlapServiceGrpc.getQueryMethod = getQueryMethod =
              io.grpc.MethodDescriptor.<olap.v1.Olap.QueryRequest, olap.v1.Olap.QueryResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(SERVICE_NAME, "Query"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  olap.v1.Olap.QueryRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  olap.v1.Olap.QueryResponse.getDefaultInstance()))
              .setSchemaDescriptor(new OlapServiceMethodDescriptorSupplier("Query"))
              .build();
        }
      }
    }
    return getQueryMethod;
  }

  private static volatile io.grpc.MethodDescriptor<olap.v1.Olap.TriggerBackupRequest,
      olap.v1.Olap.TriggerBackupResponse> getTriggerBackupMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "TriggerBackup",
      requestType = olap.v1.Olap.TriggerBackupRequest.class,
      responseType = olap.v1.Olap.TriggerBackupResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<olap.v1.Olap.TriggerBackupRequest,
      olap.v1.Olap.TriggerBackupResponse> getTriggerBackupMethod() {
    io.grpc.MethodDescriptor<olap.v1.Olap.TriggerBackupRequest, olap.v1.Olap.TriggerBackupResponse> getTriggerBackupMethod;
    if ((getTriggerBackupMethod = OlapServiceGrpc.getTriggerBackupMethod) == null) {
      synchronized (OlapServiceGrpc.class) {
        if ((getTriggerBackupMethod = OlapServiceGrpc.getTriggerBackupMethod) == null) {
          OlapServiceGrpc.getTriggerBackupMethod = getTriggerBackupMethod =
              io.grpc.MethodDescriptor.<olap.v1.Olap.TriggerBackupRequest, olap.v1.Olap.TriggerBackupResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(SERVICE_NAME, "TriggerBackup"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  olap.v1.Olap.TriggerBackupRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  olap.v1.Olap.TriggerBackupResponse.getDefaultInstance()))
              .setSchemaDescriptor(new OlapServiceMethodDescriptorSupplier("TriggerBackup"))
              .build();
        }
      }
    }
    return getTriggerBackupMethod;
  }

  private static volatile io.grpc.MethodDescriptor<olap.v1.Olap.RecoverDataRequest,
      olap.v1.Olap.RecoverDataResponse> getRecoverDataMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "RecoverData",
      requestType = olap.v1.Olap.RecoverDataRequest.class,
      responseType = olap.v1.Olap.RecoverDataResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<olap.v1.Olap.RecoverDataRequest,
      olap.v1.Olap.RecoverDataResponse> getRecoverDataMethod() {
    io.grpc.MethodDescriptor<olap.v1.Olap.RecoverDataRequest, olap.v1.Olap.RecoverDataResponse> getRecoverDataMethod;
    if ((getRecoverDataMethod = OlapServiceGrpc.getRecoverDataMethod) == null) {
      synchronized (OlapServiceGrpc.class) {
        if ((getRecoverDataMethod = OlapServiceGrpc.getRecoverDataMethod) == null) {
          OlapServiceGrpc.getRecoverDataMethod = getRecoverDataMethod =
              io.grpc.MethodDescriptor.<olap.v1.Olap.RecoverDataRequest, olap.v1.Olap.RecoverDataResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(SERVICE_NAME, "RecoverData"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  olap.v1.Olap.RecoverDataRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  olap.v1.Olap.RecoverDataResponse.getDefaultInstance()))
              .setSchemaDescriptor(new OlapServiceMethodDescriptorSupplier("RecoverData"))
              .build();
        }
      }
    }
    return getRecoverDataMethod;
  }

  private static volatile io.grpc.MethodDescriptor<olap.v1.Olap.HealthCheckRequest,
      olap.v1.Olap.HealthCheckResponse> getHealthCheckMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "HealthCheck",
      requestType = olap.v1.Olap.HealthCheckRequest.class,
      responseType = olap.v1.Olap.HealthCheckResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<olap.v1.Olap.HealthCheckRequest,
      olap.v1.Olap.HealthCheckResponse> getHealthCheckMethod() {
    io.grpc.MethodDescriptor<olap.v1.Olap.HealthCheckRequest, olap.v1.Olap.HealthCheckResponse> getHealthCheckMethod;
    if ((getHealthCheckMethod = OlapServiceGrpc.getHealthCheckMethod) == null) {
      synchronized (OlapServiceGrpc.class) {
        if ((getHealthCheckMethod = OlapServiceGrpc.getHealthCheckMethod) == null) {
          OlapServiceGrpc.getHealthCheckMethod = getHealthCheckMethod =
              io.grpc.MethodDescriptor.<olap.v1.Olap.HealthCheckRequest, olap.v1.Olap.HealthCheckResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(SERVICE_NAME, "HealthCheck"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  olap.v1.Olap.HealthCheckRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  olap.v1.Olap.HealthCheckResponse.getDefaultInstance()))
              .setSchemaDescriptor(new OlapServiceMethodDescriptorSupplier("HealthCheck"))
              .build();
        }
      }
    }
    return getHealthCheckMethod;
  }

  private static volatile io.grpc.MethodDescriptor<olap.v1.Olap.GetStatsRequest,
      olap.v1.Olap.GetStatsResponse> getGetStatsMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "GetStats",
      requestType = olap.v1.Olap.GetStatsRequest.class,
      responseType = olap.v1.Olap.GetStatsResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<olap.v1.Olap.GetStatsRequest,
      olap.v1.Olap.GetStatsResponse> getGetStatsMethod() {
    io.grpc.MethodDescriptor<olap.v1.Olap.GetStatsRequest, olap.v1.Olap.GetStatsResponse> getGetStatsMethod;
    if ((getGetStatsMethod = OlapServiceGrpc.getGetStatsMethod) == null) {
      synchronized (OlapServiceGrpc.class) {
        if ((getGetStatsMethod = OlapServiceGrpc.getGetStatsMethod) == null) {
          OlapServiceGrpc.getGetStatsMethod = getGetStatsMethod =
              io.grpc.MethodDescriptor.<olap.v1.Olap.GetStatsRequest, olap.v1.Olap.GetStatsResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(SERVICE_NAME, "GetStats"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  olap.v1.Olap.GetStatsRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  olap.v1.Olap.GetStatsResponse.getDefaultInstance()))
              .setSchemaDescriptor(new OlapServiceMethodDescriptorSupplier("GetStats"))
              .build();
        }
      }
    }
    return getGetStatsMethod;
  }

  private static volatile io.grpc.MethodDescriptor<olap.v1.Olap.GetNodesRequest,
      olap.v1.Olap.GetNodesResponse> getGetNodesMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "GetNodes",
      requestType = olap.v1.Olap.GetNodesRequest.class,
      responseType = olap.v1.Olap.GetNodesResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<olap.v1.Olap.GetNodesRequest,
      olap.v1.Olap.GetNodesResponse> getGetNodesMethod() {
    io.grpc.MethodDescriptor<olap.v1.Olap.GetNodesRequest, olap.v1.Olap.GetNodesResponse> getGetNodesMethod;
    if ((getGetNodesMethod = OlapServiceGrpc.getGetNodesMethod) == null) {
      synchronized (OlapServiceGrpc.class) {
        if ((getGetNodesMethod = OlapServiceGrpc.getGetNodesMethod) == null) {
          OlapServiceGrpc.getGetNodesMethod = getGetNodesMethod =
              io.grpc.MethodDescriptor.<olap.v1.Olap.GetNodesRequest, olap.v1.Olap.GetNodesResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(SERVICE_NAME, "GetNodes"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  olap.v1.Olap.GetNodesRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  olap.v1.Olap.GetNodesResponse.getDefaultInstance()))
              .setSchemaDescriptor(new OlapServiceMethodDescriptorSupplier("GetNodes"))
              .build();
        }
      }
    }
    return getGetNodesMethod;
  }

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static OlapServiceStub newStub(io.grpc.Channel channel) {
    io.grpc.stub.AbstractStub.StubFactory<OlapServiceStub> factory =
      new io.grpc.stub.AbstractStub.StubFactory<OlapServiceStub>() {
        @java.lang.Override
        public OlapServiceStub newStub(io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
          return new OlapServiceStub(channel, callOptions);
        }
      };
    return OlapServiceStub.newStub(factory, channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static OlapServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    io.grpc.stub.AbstractStub.StubFactory<OlapServiceBlockingStub> factory =
      new io.grpc.stub.AbstractStub.StubFactory<OlapServiceBlockingStub>() {
        @java.lang.Override
        public OlapServiceBlockingStub newStub(io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
          return new OlapServiceBlockingStub(channel, callOptions);
        }
      };
    return OlapServiceBlockingStub.newStub(factory, channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary calls on the service
   */
  public static OlapServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    io.grpc.stub.AbstractStub.StubFactory<OlapServiceFutureStub> factory =
      new io.grpc.stub.AbstractStub.StubFactory<OlapServiceFutureStub>() {
        @java.lang.Override
        public OlapServiceFutureStub newStub(io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
          return new OlapServiceFutureStub(channel, callOptions);
        }
      };
    return OlapServiceFutureStub.newStub(factory, channel);
  }

  /**
   */
  public static abstract class OlapServiceImplBase implements io.grpc.BindableService {

    /**
     * <pre>
     * 写入数据
     * </pre>
     */
    public void write(olap.v1.Olap.WriteRequest request,
        io.grpc.stub.StreamObserver<olap.v1.Olap.WriteResponse> responseObserver) {
      io.grpc.stub.ServerCalls.asyncUnimplementedUnaryCall(getWriteMethod(), responseObserver);
    }

    /**
     * <pre>
     * 执行查询
     * </pre>
     */
    public void query(olap.v1.Olap.QueryRequest request,
        io.grpc.stub.StreamObserver<olap.v1.Olap.QueryResponse> responseObserver) {
      io.grpc.stub.ServerCalls.asyncUnimplementedUnaryCall(getQueryMethod(), responseObserver);
    }

    /**
     * <pre>
     * 手动触发数据备份
     * </pre>
     */
    public void triggerBackup(olap.v1.Olap.TriggerBackupRequest request,
        io.grpc.stub.StreamObserver<olap.v1.Olap.TriggerBackupResponse> responseObserver) {
      io.grpc.stub.ServerCalls.asyncUnimplementedUnaryCall(getTriggerBackupMethod(), responseObserver);
    }

    /**
     * <pre>
     * 从备份节点恢复数据
     * </pre>
     */
    public void recoverData(olap.v1.Olap.RecoverDataRequest request,
        io.grpc.stub.StreamObserver<olap.v1.Olap.RecoverDataResponse> responseObserver) {
      io.grpc.stub.ServerCalls.asyncUnimplementedUnaryCall(getRecoverDataMethod(), responseObserver);
    }

    /**
     * <pre>
     * 健康检查
     * </pre>
     */
    public void healthCheck(olap.v1.Olap.HealthCheckRequest request,
        io.grpc.stub.StreamObserver<olap.v1.Olap.HealthCheckResponse> responseObserver) {
      io.grpc.stub.ServerCalls.asyncUnimplementedUnaryCall(getHealthCheckMethod(), responseObserver);
    }

    /**
     * <pre>
     * 获取系统统计信息
     * </pre>
     */
    public void getStats(olap.v1.Olap.GetStatsRequest request,
        io.grpc.stub.StreamObserver<olap.v1.Olap.GetStatsResponse> responseObserver) {
      io.grpc.stub.ServerCalls.asyncUnimplementedUnaryCall(getGetStatsMethod(), responseObserver);
    }

    /**
     * <pre>
     * 获取集群节点信息
     * </pre>
     */
    public void getNodes(olap.v1.Olap.GetNodesRequest request,
        io.grpc.stub.StreamObserver<olap.v1.Olap.GetNodesResponse> responseObserver) {
      io.grpc.stub.ServerCalls.asyncUnimplementedUnaryCall(getGetNodesMethod(), responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            getWriteMethod(),
            io.grpc.stub.ServerCalls.asyncUnaryCall(
              new MethodHandlers<
                olap.v1.Olap.WriteRequest,
                olap.v1.Olap.WriteResponse>(
                  this, METHODID_WRITE)))
          .addMethod(
            getQueryMethod(),
            io.grpc.stub.ServerCalls.asyncUnaryCall(
              new MethodHandlers<
                olap.v1.Olap.QueryRequest,
                olap.v1.Olap.QueryResponse>(
                  this, METHODID_QUERY)))
          .addMethod(
            getTriggerBackupMethod(),
            io.grpc.stub.ServerCalls.asyncUnaryCall(
              new MethodHandlers<
                olap.v1.Olap.TriggerBackupRequest,
                olap.v1.Olap.TriggerBackupResponse>(
                  this, METHODID_TRIGGER_BACKUP)))
          .addMethod(
            getRecoverDataMethod(),
            io.grpc.stub.ServerCalls.asyncUnaryCall(
              new MethodHandlers<
                olap.v1.Olap.RecoverDataRequest,
                olap.v1.Olap.RecoverDataResponse>(
                  this, METHODID_RECOVER_DATA)))
          .addMethod(
            getHealthCheckMethod(),
            io.grpc.stub.ServerCalls.asyncUnaryCall(
              new MethodHandlers<
                olap.v1.Olap.HealthCheckRequest,
                olap.v1.Olap.HealthCheckResponse>(
                  this, METHODID_HEALTH_CHECK)))
          .addMethod(
            getGetStatsMethod(),
            io.grpc.stub.ServerCalls.asyncUnaryCall(
              new MethodHandlers<
                olap.v1.Olap.GetStatsRequest,
                olap.v1.Olap.GetStatsResponse>(
                  this, METHODID_GET_STATS)))
          .addMethod(
            getGetNodesMethod(),
            io.grpc.stub.ServerCalls.asyncUnaryCall(
              new MethodHandlers<
                olap.v1.Olap.GetNodesRequest,
                olap.v1.Olap.GetNodesResponse>(
                  this, METHODID_GET_NODES)))
          .build();
    }
  }

  /**
   */
  public static final class OlapServiceStub extends io.grpc.stub.AbstractAsyncStub<OlapServiceStub> {
    private OlapServiceStub(
        io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected OlapServiceStub build(
        io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
      return new OlapServiceStub(channel, callOptions);
    }

    /**
     * <pre>
     * 写入数据
     * </pre>
     */
    public void write(olap.v1.Olap.WriteRequest request,
        io.grpc.stub.StreamObserver<olap.v1.Olap.WriteResponse> responseObserver) {
      io.grpc.stub.ClientCalls.asyncUnaryCall(
          getChannel().newCall(getWriteMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * 执行查询
     * </pre>
     */
    public void query(olap.v1.Olap.QueryRequest request,
        io.grpc.stub.StreamObserver<olap.v1.Olap.QueryResponse> responseObserver) {
      io.grpc.stub.ClientCalls.asyncUnaryCall(
          getChannel().newCall(getQueryMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * 手动触发数据备份
     * </pre>
     */
    public void triggerBackup(olap.v1.Olap.TriggerBackupRequest request,
        io.grpc.stub.StreamObserver<olap.v1.Olap.TriggerBackupResponse> responseObserver) {
      io.grpc.stub.ClientCalls.asyncUnaryCall(
          getChannel().newCall(getTriggerBackupMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * 从备份节点恢复数据
     * </pre>
     */
    public void recoverData(olap.v1.Olap.RecoverDataRequest request,
        io.grpc.stub.StreamObserver<olap.v1.Olap.RecoverDataResponse> responseObserver) {
      io.grpc.stub.ClientCalls.asyncUnaryCall(
          getChannel().newCall(getRecoverDataMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * 健康检查
     * </pre>
     */
    public void healthCheck(olap.v1.Olap.HealthCheckRequest request,
        io.grpc.stub.StreamObserver<olap.v1.Olap.HealthCheckResponse> responseObserver) {
      io.grpc.stub.ClientCalls.asyncUnaryCall(
          getChannel().newCall(getHealthCheckMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * 获取系统统计信息
     * </pre>
     */
    public void getStats(olap.v1.Olap.GetStatsRequest request,
        io.grpc.stub.StreamObserver<olap.v1.Olap.GetStatsResponse> responseObserver) {
      io.grpc.stub.ClientCalls.asyncUnaryCall(
          getChannel().newCall(getGetStatsMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     * <pre>
     * 获取集群节点信息
     * </pre>
     */
    public void getNodes(olap.v1.Olap.GetNodesRequest request,
        io.grpc.stub.StreamObserver<olap.v1.Olap.GetNodesResponse> responseObserver) {
      io.grpc.stub.ClientCalls.asyncUnaryCall(
          getChannel().newCall(getGetNodesMethod(), getCallOptions()), request, responseObserver);
    }
  }

  /**
   */
  public static final class OlapServiceBlockingStub extends io.grpc.stub.AbstractBlockingStub<OlapServiceBlockingStub> {
    private OlapServiceBlockingStub(
        io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected OlapServiceBlockingStub build(
        io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
      return new OlapServiceBlockingStub(channel, callOptions);
    }

    /**
     * <pre>
     * 写入数据
     * </pre>
     */
    public olap.v1.Olap.WriteResponse write(olap.v1.Olap.WriteRequest request) {
      return io.grpc.stub.ClientCalls.blockingUnaryCall(
          getChannel(), getWriteMethod(), getCallOptions(), request);
    }

    /**
     * <pre>
     * 执行查询
     * </pre>
     */
    public olap.v1.Olap.QueryResponse query(olap.v1.Olap.QueryRequest request) {
      return io.grpc.stub.ClientCalls.blockingUnaryCall(
          getChannel(), getQueryMethod(), getCallOptions(), request);
    }

    /**
     * <pre>
     * 手动触发数据备份
     * </pre>
     */
    public olap.v1.Olap.TriggerBackupResponse triggerBackup(olap.v1.Olap.TriggerBackupRequest request) {
      return io.grpc.stub.ClientCalls.blockingUnaryCall(
          getChannel(), getTriggerBackupMethod(), getCallOptions(), request);
    }

    /**
     * <pre>
     * 从备份节点恢复数据
     * </pre>
     */
    public olap.v1.Olap.RecoverDataResponse recoverData(olap.v1.Olap.RecoverDataRequest request) {
      return io.grpc.stub.ClientCalls.blockingUnaryCall(
          getChannel(), getRecoverDataMethod(), getCallOptions(), request);
    }

    /**
     * <pre>
     * 健康检查
     * </pre>
     */
    public olap.v1.Olap.HealthCheckResponse healthCheck(olap.v1.Olap.HealthCheckRequest request) {
      return io.grpc.stub.ClientCalls.blockingUnaryCall(
          getChannel(), getHealthCheckMethod(), getCallOptions(), request);
    }

    /**
     * <pre>
     * 获取系统统计信息
     * </pre>
     */
    public olap.v1.Olap.GetStatsResponse getStats(olap.v1.Olap.GetStatsRequest request) {
      return io.grpc.stub.ClientCalls.blockingUnaryCall(
          getChannel(), getGetStatsMethod(), getCallOptions(), request);
    }

    /**
     * <pre>
     * 获取集群节点信息
     * </pre>
     */
    public olap.v1.Olap.GetNodesResponse getNodes(olap.v1.Olap.GetNodesRequest request) {
      return io.grpc.stub.ClientCalls.blockingUnaryCall(
          getChannel(), getGetNodesMethod(), getCallOptions(), request);
    }
  }

  /**
   */
  public static final class OlapServiceFutureStub extends io.grpc.stub.AbstractFutureStub<OlapServiceFutureStub> {
    private OlapServiceFutureStub(
        io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected OlapServiceFutureStub build(
        io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
      return new OlapServiceFutureStub(channel, callOptions);
    }

    /**
     * <pre>
     * 写入数据
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<olap.v1.Olap.WriteResponse> write(
        olap.v1.Olap.WriteRequest request) {
      return io.grpc.stub.ClientCalls.futureUnaryCall(
          getChannel().newCall(getWriteMethod(), getCallOptions()), request);
    }

    /**
     * <pre>
     * 执行查询
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<olap.v1.Olap.QueryResponse> query(
        olap.v1.Olap.QueryRequest request) {
      return io.grpc.stub.ClientCalls.futureUnaryCall(
          getChannel().newCall(getQueryMethod(), getCallOptions()), request);
    }

    /**
     * <pre>
     * 手动触发数据备份
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<olap.v1.Olap.TriggerBackupResponse> triggerBackup(
        olap.v1.Olap.TriggerBackupRequest request) {
      return io.grpc.stub.ClientCalls.futureUnaryCall(
          getChannel().newCall(getTriggerBackupMethod(), getCallOptions()), request);
    }

    /**
     * <pre>
     * 从备份节点恢复数据
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<olap.v1.Olap.RecoverDataResponse> recoverData(
        olap.v1.Olap.RecoverDataRequest request) {
      return io.grpc.stub.ClientCalls.futureUnaryCall(
          getChannel().newCall(getRecoverDataMethod(), getCallOptions()), request);
    }

    /**
     * <pre>
     * 健康检查
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<olap.v1.Olap.HealthCheckResponse> healthCheck(
        olap.v1.Olap.HealthCheckRequest request) {
      return io.grpc.stub.ClientCalls.futureUnaryCall(
          getChannel().newCall(getHealthCheckMethod(), getCallOptions()), request);
    }

    /**
     * <pre>
     * 获取系统统计信息
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<olap.v1.Olap.GetStatsResponse> getStats(
        olap.v1.Olap.GetStatsRequest request) {
      return io.grpc.stub.ClientCalls.futureUnaryCall(
          getChannel().newCall(getGetStatsMethod(), getCallOptions()), request);
    }

    /**
     * <pre>
     * 获取集群节点信息
     * </pre>
     */
    public com.google.common.util.concurrent.ListenableFuture<olap.v1.Olap.GetNodesResponse> getNodes(
        olap.v1.Olap.GetNodesRequest request) {
      return io.grpc.stub.ClientCalls.futureUnaryCall(
          getChannel().newCall(getGetNodesMethod(), getCallOptions()), request);
    }
  }

  private static final int METHODID_WRITE = 0;
  private static final int METHODID_QUERY = 1;
  private static final int METHODID_TRIGGER_BACKUP = 2;
  private static final int METHODID_RECOVER_DATA = 3;
  private static final int METHODID_HEALTH_CHECK = 4;
  private static final int METHODID_GET_STATS = 5;
  private static final int METHODID_GET_NODES = 6;

  private static final class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final OlapServiceImplBase serviceImpl;
    private final int methodId;

    MethodHandlers(OlapServiceImplBase serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_WRITE:
          serviceImpl.write((olap.v1.Olap.WriteRequest) request,
              (io.grpc.stub.StreamObserver<olap.v1.Olap.WriteResponse>) responseObserver);
          break;
        case METHODID_QUERY:
          serviceImpl.query((olap.v1.Olap.QueryRequest) request,
              (io.grpc.stub.StreamObserver<olap.v1.Olap.QueryResponse>) responseObserver);
          break;
        case METHODID_TRIGGER_BACKUP:
          serviceImpl.triggerBackup((olap.v1.Olap.TriggerBackupRequest) request,
              (io.grpc.stub.StreamObserver<olap.v1.Olap.TriggerBackupResponse>) responseObserver);
          break;
        case METHODID_RECOVER_DATA:
          serviceImpl.recoverData((olap.v1.Olap.RecoverDataRequest) request,
              (io.grpc.stub.StreamObserver<olap.v1.Olap.RecoverDataResponse>) responseObserver);
          break;
        case METHODID_HEALTH_CHECK:
          serviceImpl.healthCheck((olap.v1.Olap.HealthCheckRequest) request,
              (io.grpc.stub.StreamObserver<olap.v1.Olap.HealthCheckResponse>) responseObserver);
          break;
        case METHODID_GET_STATS:
          serviceImpl.getStats((olap.v1.Olap.GetStatsRequest) request,
              (io.grpc.stub.StreamObserver<olap.v1.Olap.GetStatsResponse>) responseObserver);
          break;
        case METHODID_GET_NODES:
          serviceImpl.getNodes((olap.v1.Olap.GetNodesRequest) request,
              (io.grpc.stub.StreamObserver<olap.v1.Olap.GetNodesResponse>) responseObserver);
          break;
        default:
          throw new AssertionError();
      }
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public io.grpc.stub.StreamObserver<Req> invoke(
        io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        default:
          throw new AssertionError();
      }
    }
  }

  private static abstract class OlapServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoFileDescriptorSupplier, io.grpc.protobuf.ProtoServiceDescriptorSupplier {
    OlapServiceBaseDescriptorSupplier() {}

    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return olap.v1.Olap.getDescriptor();
    }

    @java.lang.Override
    public com.google.protobuf.Descriptors.ServiceDescriptor getServiceDescriptor() {
      return getFileDescriptor().findServiceByName("OlapService");
    }
  }

  private static final class OlapServiceFileDescriptorSupplier
      extends OlapServiceBaseDescriptorSupplier {
    OlapServiceFileDescriptorSupplier() {}
  }

  private static final class OlapServiceMethodDescriptorSupplier
      extends OlapServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoMethodDescriptorSupplier {
    private final String methodName;

    OlapServiceMethodDescriptorSupplier(String methodName) {
      this.methodName = methodName;
    }

    @java.lang.Override
    public com.google.protobuf.Descriptors.MethodDescriptor getMethodDescriptor() {
      return getServiceDescriptor().findMethodByName(methodName);
    }
  }

  private static volatile io.grpc.ServiceDescriptor serviceDescriptor;

  public static io.grpc.ServiceDescriptor getServiceDescriptor() {
    io.grpc.ServiceDescriptor result = serviceDescriptor;
    if (result == null) {
      synchronized (OlapServiceGrpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .setSchemaDescriptor(new OlapServiceFileDescriptorSupplier())
              .addMethod(getWriteMethod())
              .addMethod(getQueryMethod())
              .addMethod(getTriggerBackupMethod())
              .addMethod(getRecoverDataMethod())
              .addMethod(getHealthCheckMethod())
              .addMethod(getGetStatsMethod())
              .addMethod(getGetNodesMethod())
              .build();
        }
      }
    }
    return result;
  }
}
