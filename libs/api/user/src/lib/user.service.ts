import { ConflictException, Injectable, NotFoundException } from '@nestjs/common';
import { InjectModel } from '@nestjs/mongoose';
import * as bcrypt from 'bcrypt';
import type { Model } from 'mongoose';

import type { CreateUserDto } from './dto/create-user.dto';
import type { UpdateUserDto } from './dto/update-user.dto';
import { User, type UserDocument } from './schemas/user.schema';

@Injectable()
export class UserService {
  constructor(@InjectModel(User.name) private readonly userModel: Model<UserDocument>) {}

  async create(createUserDto: CreateUserDto): Promise<{ id: string; username: string; email: string }> {
    const userExists = await this.userModel.findOne({
      email: createUserDto.email,
      username: createUserDto.username,
    });

    if (userExists) {
      throw new ConflictException('Email and/or username already exists.');
    }

    const userToCreate = {
      username: createUserDto.username,
      email: createUserDto.email,
      password: await this.hashPassword(createUserDto.password),
      avatar: createUserDto.avatar,
    };

    const createdUser = await this.userModel.create(userToCreate);

    // TODO - handle if isAdmin in JWT
    return {
      id: createdUser._id,
      username: createdUser.username,
      email: createdUser.email,
    };
  }

  async findAll(): Promise<User[]> {
    return this.userModel.find().exec();
  }

  async findOne(id: string): Promise<User | undefined> {
    const user = await this.userModel.findOne({ _id: id });

    if (user) {
      return user;
    } else {
      throw new NotFoundException('User not found.');
    }
  }

  async findOneByUsername(username: string): Promise<User | undefined> {
    const user = await this.userModel.findOne({ username });

    if (user) {
      return user;
    } else {
      throw new NotFoundException('User not found.');
    }
  }

  async findOneByEmail(email: string): Promise<User | undefined> {
    const user = await this.userModel.findOne({ email });

    if (user) {
      return user;
    } else {
      throw new NotFoundException('User not found.');
    }
  }

  async update(id: string, updateUserDto: UpdateUserDto): Promise<User | undefined | null> {
    const user = await this.findOne(id);

    if (user) {
      return this.userModel.findByIdAndUpdate({ _id: id }, updateUserDto).exec();
    } else {
      throw new NotFoundException('User not found.');
    }
  }

  async remove(id: string) {
    const deletedUser = await this.userModel.findByIdAndRemove({ _id: id }).exec();
    return deletedUser;
  }

  private async hashPassword(password: string): Promise<string> {
    return bcrypt.hash(password, 12);
  }
}
