import { DeleteObjectCommand, S3Client } from '@aws-sdk/client-s3';
import {
  BadRequestException,
  ConflictException,
  ForbiddenException,
  Injectable,
  Logger,
  NotFoundException,
} from '@nestjs/common';
import { ConfigService } from '@nestjs/config';
import { InjectModel } from '@nestjs/mongoose';
import * as bcrypt from 'bcrypt';
import { Model, Types } from 'mongoose';

import type { UserRole, UserStatus } from '@shared/types';

import { CreateUserDto } from './dto/create-user.dto';
import { UpdateUserDto } from './dto/update-user.dto';
import { User, UserDocument } from './schemas/user.schema';
import { hasAdminRole, resolveStatus } from './authorization/role.helpers';
import type { AuthActor } from './types/auth-actor.types';

export interface FavoriteBlogPost {
  _id: string;
  title: string;
  subtitle: string;
  description: string;
  thumbnail: string;
  downloadLink: string;
  downloadLinkTorrent: string;
  downloadLink4k?: string;
  downloadLink4kTorrent?: string;
  dateTimeCreated: string;
  creator?: unknown;
}

export interface FavoriteBlogPostsResponse {
  posts: FavoriteBlogPost[];
  count: number;
}

export interface FindManagementUsersQuery {
  page?: number;
  pageSize?: number;
  search?: string;
  role?: UserRole;
  status?: UserStatus;
  sortBy?: 'username' | 'email' | 'role' | 'status' | 'createdAt' | 'updatedAt';
  sortDirection?: 'asc' | 'desc';
}

export interface FindManagementUsersResponse {
  users: Array<ReturnType<UserService['toPublicUser']>>;
  count: number;
  page: number;
  pageSize: number;
}

@Injectable()
export class UserService {
  private toRoleAndStatus(user: { role?: UserRole; status?: UserStatus }): { role: UserRole; status: UserStatus } {
    const role = user.role ?? 'user';
    const status = resolveStatus(user.status);
    return { role, status };
  }

  private readonly logger = new Logger(UserService.name);
  private readonly minioBucket: string;
  private readonly minioClient: S3Client;

  constructor(
    @InjectModel(User.name) private readonly userModel: Model<UserDocument>,
    private readonly configService: ConfigService,
  ) {
    const endpoint = this.configService.get<string>('MINIO_ENDPOINT', 'http://minio:9000');
    const accessKeyId = this.configService.get<string>('MINIO_ACCESS_KEY', 'minioadmin');
    const secretAccessKey = this.configService.get<string>('MINIO_SECRET_KEY', 'minioadmin');
    const region = this.configService.get<string>('MINIO_REGION', 'us-east-1');

    this.minioBucket = this.configService.get<string>('MINIO_BUCKET', 'images');
    this.minioClient = new S3Client({
      endpoint,
      region,
      credentials: { accessKeyId, secretAccessKey },
      forcePathStyle: true,
    });
  }

  private parseUrl(url: string | undefined): URL | null {
    const value = url?.trim();
    if (!value) return null;

    try {
      return new URL(value, 'http://localhost');
    } catch {
      return null;
    }
  }

  private extractManagedAvatarKey(url: string | undefined): string | null {
    const parsed = this.parseUrl(url);
    if (!parsed) return null;

    const prefix = '/media/images/';
    if (!parsed.pathname.startsWith(prefix)) {
      return null;
    }

    const key = parsed.pathname.slice(prefix.length).trim();
    return key || null;
  }

  private async deleteManagedAvatarByKey(key: string): Promise<void> {
    try {
      await this.minioClient.send(
        new DeleteObjectCommand({
          Bucket: this.minioBucket,
          Key: key,
        }),
      );
    } catch (error) {
      this.logger.warn(`Failed to delete previous avatar object ${key}`, error as Error);
    }
  }

  private assertValidId(id: string): void {
    if (!Types.ObjectId.isValid(id)) {
      throw new BadRequestException('Invalid user id.');
    }
  }

  private assertAdmin(actor: AuthActor): void {
    if (hasAdminRole(actor.role)) {
      return;
    }

    throw new ForbiddenException('Insufficient permissions.');
  }

  private assertDashboardReader(actor: AuthActor): void {
    if (hasAdminRole(actor.role) || actor.role === 'moderator') return;

    throw new ForbiddenException('Insufficient permissions.');
  }

  private assertCanAccessUser(id: string, actor: AuthActor): void {
    if (hasAdminRole(actor.role) || actor.sub === id) {
      return;
    }

    throw new ForbiddenException('You are not allowed to access this user.');
  }

  private toPublicUser(user: UserDocument): {
    id: string;
    username: string;
    email: string;
    avatar?: string;
    role: UserRole;
    status: UserStatus;
    favoriteBlogPostIds: string[];
    createdBlogPostIds: string[];
    createdAt?: Date;
    updatedAt?: Date;
  } {
    const serialized = user.toObject({ virtuals: false });

    const favoriteBlogPostIds = Array.isArray(serialized.favoriteBlogPostIds)
      ? serialized.favoriteBlogPostIds.map((postId: Types.ObjectId | string) => postId.toString())
      : [];

    const createdBlogPostIds = Array.isArray(serialized.createdBlogPostIds)
      ? serialized.createdBlogPostIds.map((postId: Types.ObjectId | string) => postId.toString())
      : [];

    const identity = this.toRoleAndStatus(serialized);

    return {
      id: user._id.toString(),
      username: serialized.username,
      email: serialized.email,
      avatar: serialized.avatar,
      role: identity.role,
      status: identity.status,
      favoriteBlogPostIds,
      createdBlogPostIds,
      createdAt: serialized.createdAt,
      updatedAt: serialized.updatedAt,
    };
  }

  private assertValidBlogPostId(postId: string): void {
    if (!Types.ObjectId.isValid(postId)) {
      throw new BadRequestException('Invalid blog post id.');
    }
  }

  private async assertBlogPostExists(postId: string): Promise<void> {
    const blogPostsCollection = this.userModel.db.collection('blogposts');
    const existingPost = await blogPostsCollection.findOne({ _id: new Types.ObjectId(postId) }, { projection: { _id: 1 } });

    if (!existingPost) {
      throw new NotFoundException('Blog post not found.');
    }
  }

  private serializeFavoriteBlogPost(rawPost: Record<string, unknown>): FavoriteBlogPost {
    return {
      _id: (rawPost['_id'] as Types.ObjectId).toString(),
      title: String(rawPost['title'] ?? ''),
      subtitle: String(rawPost['subtitle'] ?? ''),
      description: String(rawPost['description'] ?? ''),
      thumbnail: String(rawPost['thumbnail'] ?? ''),
      downloadLink: String(rawPost['downloadLink'] ?? ''),
      downloadLinkTorrent: String(rawPost['downloadLinkTorrent'] ?? ''),
      downloadLink4k: rawPost['downloadLink4k'] ? String(rawPost['downloadLink4k']) : undefined,
      downloadLink4kTorrent: rawPost['downloadLink4kTorrent'] ? String(rawPost['downloadLink4kTorrent']) : undefined,
      dateTimeCreated: String(rawPost['dateTimeCreated'] ?? ''),
      creator: rawPost['creator'],
    };
  }

  async create(createUserDto: CreateUserDto): Promise<{ id: string; username: string; email: string }> {
    const userExists = await this.userModel.findOne({
      $or: [{ email: createUserDto.email }, { username: createUserDto.username }],
    });

    if (userExists) {
      throw new ConflictException('Email and/or username already exists.');
    }

    const userToCreate = {
      username: createUserDto.username,
      email: createUserDto.email,
      password: await this.hashPassword(createUserDto.password),
      avatar: createUserDto.avatar,
      role: 'user' as const,
      status: 'active' as const,
    };

    const createdUser = await this.userModel.create(userToCreate);

    return {
      id: createdUser._id.toString(),
      username: createdUser.username,
      email: createdUser.email,
    };
  }

  async findAll(actor: AuthActor): Promise<Array<ReturnType<UserService['toPublicUser']>>> {
    this.assertDashboardReader(actor);

    const users = await this.userModel.find().exec();
    return users.map((user) => this.toPublicUser(user));
  }

  async findManagementUsers(actor: AuthActor, query: FindManagementUsersQuery): Promise<FindManagementUsersResponse> {
    this.assertDashboardReader(actor);

    const page = Number.isFinite(query.page) && (query.page ?? 0) > 0 ? Math.floor(query.page as number) : 1;
    const pageSize =
      Number.isFinite(query.pageSize) && (query.pageSize ?? 0) > 0
        ? Math.min(Math.floor(query.pageSize as number), 100)
        : 10;

    const normalizedSearch = query.search?.trim();
    const filters: {
      $or?: Array<{ username?: { $regex: string; $options: string }; email?: { $regex: string; $options: string } }>;
      role?: UserRole;
      status?: UserStatus;
    } = {};

    if (normalizedSearch) {
      filters.$or = [
        { username: { $regex: normalizedSearch, $options: 'i' } },
        { email: { $regex: normalizedSearch, $options: 'i' } },
      ];
    }

    if (query.role) {
      filters.role = query.role;
    }

    if (query.status) {
      filters.status = query.status;
    }

    const sortBy = query.sortBy ?? 'updatedAt';
    const sortDirection = query.sortDirection === 'asc' ? 1 : -1;
    const sort: Record<string, 1 | -1> = { [sortBy]: sortDirection };

    const [count, users] = await Promise.all([
      this.userModel.countDocuments(filters).exec(),
      this.userModel
        .find(filters)
        .sort(sort)
        .skip((page - 1) * pageSize)
        .limit(pageSize)
        .exec(),
    ]);

    return {
      users: users.map((user) => this.toPublicUser(user)),
      count,
      page,
      pageSize,
    };
  }

  async findOneEntityById(id: string): Promise<UserDocument> {
    this.assertValidId(id);

    const user = await this.userModel.findById(id).exec();
    if (user) return user;
    throw new NotFoundException('User not found.');
  }

  async findOne(id: string, actor: AuthActor): Promise<ReturnType<UserService['toPublicUser']>> {
    this.assertCanAccessUser(id, actor);
    const user = await this.findOneEntityById(id);
    return this.toPublicUser(user);
  }

  async findOneByUsername(username: string): Promise<UserDocument> {
    const user = await this.userModel.findOne({ username });
    if (user) return user;
    throw new NotFoundException('User not found.');
  }

  async findOneByEmail(email: string): Promise<UserDocument> {
    const user = await this.userModel.findOne({ email });
    if (user) return user;
    throw new NotFoundException('User not found.');
  }

  async update(
    id: string,
    updateUserDto: UpdateUserDto,
    actor: AuthActor,
  ): Promise<ReturnType<UserService['toPublicUser']>> {
    this.assertCanAccessUser(id, actor);
    this.assertValidId(id);

    if (updateUserDto.email) {
      const emailInUse = await this.userModel.findOne({ email: updateUserDto.email, _id: { $ne: id } }).exec();
      if (emailInUse) {
        throw new ConflictException('Email already exists.');
      }
    }

    const hashedPassword = updateUserDto.password ? await this.hashPassword(updateUserDto.password) : undefined;

    const updatePayload: { email?: string; avatar?: string; password?: string } = {
      email: updateUserDto.email,
      avatar: updateUserDto.avatar,
      ...(hashedPassword ? { password: hashedPassword } : {}),
    };

    const updatedUser = await this.userModel.findByIdAndUpdate(id, updatePayload, { new: true, runValidators: true }).exec();

    if (!updatedUser) {
      throw new NotFoundException('User not found.');
    }

    if (hashedPassword) {
      await this.clearRefreshTokenSession(id);
    }

    const existingUser = await this.findOneEntityById(id);
    const previousAvatarKey = this.extractManagedAvatarKey(existingUser.avatar);
    const nextAvatarKey = this.extractManagedAvatarKey(updatedUser.avatar);
    const avatarChanged = existingUser.avatar !== updatedUser.avatar;

    if (avatarChanged && previousAvatarKey && previousAvatarKey !== nextAvatarKey) {
      await this.deleteManagedAvatarByKey(previousAvatarKey);
    }

    return this.toPublicUser(updatedUser);
  }

  async remove(id: string, actor: AuthActor) {
    this.assertCanAccessUser(id, actor);
    this.assertValidId(id);

    const deletedUser = await this.userModel.findByIdAndDelete(id).exec();
    if (!deletedUser) {
      throw new NotFoundException('User not found.');
    }

    return this.toPublicUser(deletedUser);
  }

  async getFavoriteBlogPostIds(id: string, actor: AuthActor): Promise<{ favoriteBlogPostIds: string[] }> {
    this.assertCanAccessUser(id, actor);
    const user = await this.findOneEntityById(id);

    return {
      favoriteBlogPostIds: this.toPublicUser(user).favoriteBlogPostIds,
    };
  }

  async getFavoriteBlogPosts(
    id: string,
    actor: AuthActor,
    pageSize = 10,
    currentPage = 1,
  ): Promise<FavoriteBlogPostsResponse> {
    this.assertCanAccessUser(id, actor);
    const user = await this.findOneEntityById(id);
    const favoriteBlogPostIds = this.toPublicUser(user).favoriteBlogPostIds;
    const count = favoriteBlogPostIds.length;

    const normalizedPageSize = Number.isFinite(pageSize) && pageSize > 0 ? Math.floor(pageSize) : 10;
    const normalizedCurrentPage = Number.isFinite(currentPage) && currentPage > 0 ? Math.floor(currentPage) : 1;
    const startIndex = (normalizedCurrentPage - 1) * normalizedPageSize;
    const paginatedFavoriteIds = favoriteBlogPostIds.slice(startIndex, startIndex + normalizedPageSize);

    if (!paginatedFavoriteIds.length) {
      return { posts: [], count };
    }

    const objectIds = paginatedFavoriteIds.map((postId) => new Types.ObjectId(postId));
    const rawPosts = (await this.userModel.db
      .collection('blogposts')
      .find({ _id: { $in: objectIds } })
      .toArray()) as Array<Record<string, unknown>>;

    const postsById = new Map(rawPosts.map((rawPost) => [String((rawPost['_id'] as Types.ObjectId).toString()), rawPost]));

    return {
      count,
      posts: paginatedFavoriteIds
        .map((postId) => postsById.get(postId))
        .filter((rawPost): rawPost is Record<string, unknown> => Boolean(rawPost))
        .map((rawPost) => this.serializeFavoriteBlogPost(rawPost)),
    };
  }

  async addFavoriteBlogPost(id: string, postId: string, actor: AuthActor): Promise<{ favoriteBlogPostIds: string[] }> {
    this.assertCanAccessUser(id, actor);
    this.assertValidId(id);
    this.assertValidBlogPostId(postId);

    await this.findOneEntityById(id);
    await this.assertBlogPostExists(postId);

    const updatedUser = await this.userModel
      .findByIdAndUpdate(
        id,
        { $addToSet: { favoriteBlogPostIds: new Types.ObjectId(postId) } },
        { new: true, runValidators: true },
      )
      .exec();

    if (!updatedUser) {
      throw new NotFoundException('User not found.');
    }

    return {
      favoriteBlogPostIds: this.toPublicUser(updatedUser).favoriteBlogPostIds,
    };
  }

  async removeFavoriteBlogPost(id: string, postId: string, actor: AuthActor): Promise<{ favoriteBlogPostIds: string[] }> {
    this.assertCanAccessUser(id, actor);
    this.assertValidId(id);
    this.assertValidBlogPostId(postId);

    await this.findOneEntityById(id);

    const updatedUser = await this.userModel
      .findByIdAndUpdate(
        id,
        { $pull: { favoriteBlogPostIds: new Types.ObjectId(postId) } },
        { new: true, runValidators: true },
      )
      .exec();

    if (!updatedUser) {
      throw new NotFoundException('User not found.');
    }

    return {
      favoriteBlogPostIds: this.toPublicUser(updatedUser).favoriteBlogPostIds,
    };
  }

  async addCreatedBlogPost(id: string, postId: string): Promise<void> {
    this.assertValidId(id);
    this.assertValidBlogPostId(postId);

    const updatedUser = await this.userModel
      .findByIdAndUpdate(id, { $addToSet: { createdBlogPostIds: new Types.ObjectId(postId) } }, { new: true })
      .exec();

    if (!updatedUser) {
      throw new NotFoundException('User not found.');
    }
  }

  async removeCreatedBlogPost(id: string, postId: string): Promise<void> {
    this.assertValidId(id);
    this.assertValidBlogPostId(postId);

    const updatedUser = await this.userModel
      .findByIdAndUpdate(id, { $pull: { createdBlogPostIds: new Types.ObjectId(postId) } }, { new: true })
      .exec();

    if (!updatedUser) {
      throw new NotFoundException('User not found.');
    }
  }

  async storeRefreshTokenSession(
    id: string,
    refreshToken: string,
    refreshTokenJti: string,
    refreshTokenExpiresAt: Date,
  ): Promise<void> {
    this.assertValidId(id);

    const refreshTokenHash = await this.hashPassword(refreshToken);

    const updatedUser = await this.userModel
      .findByIdAndUpdate(
        id,
        {
          refreshTokenHash,
          refreshTokenJti,
          refreshTokenExpiresAt,
        },
        { new: true, runValidators: true },
      )
      .exec();

    if (!updatedUser) {
      throw new NotFoundException('User not found.');
    }
  }

  async clearRefreshTokenSession(id: string): Promise<void> {
    this.assertValidId(id);

    const updatedUser = await this.userModel
      .findByIdAndUpdate(
        id,
        {
          $unset: {
            refreshTokenHash: '',
            refreshTokenJti: '',
            refreshTokenExpiresAt: '',
          },
        },
        { new: true },
      )
      .exec();

    if (!updatedUser) {
      throw new NotFoundException('User not found.');
    }
  }

  async isRefreshTokenSessionValid(id: string, refreshToken: string, refreshTokenJti: string): Promise<boolean> {
    this.assertValidId(id);

    const user = await this.findOneEntityById(id);

    if (!user.refreshTokenHash || !user.refreshTokenJti || !user.refreshTokenExpiresAt) return false;
    if (user.refreshTokenJti !== refreshTokenJti) return false;
    if (user.refreshTokenExpiresAt.getTime() <= Date.now()) return false;

    return this.comparePasswords(refreshToken, user.refreshTokenHash);
  }

  private async comparePasswords(password: string, storedPasswordHash: string): Promise<boolean> {
    return bcrypt.compare(password, storedPasswordHash);
  }

  private async hashPassword(password: string): Promise<string> {
    return bcrypt.hash(password, 12);
  }
}
